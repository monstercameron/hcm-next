package pgxadapter_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

func TestTodo_PERFOPT_001(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	pool, err := pgxadapter.NewPool(ctx, db.URL+"&pool_max_conns=1", map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	baseline := pool.Stats().HygieneRoundTrips
	if err := pool.WithConn(ctx, func(dbport.Conn) error { return nil }); err != nil {
		t.Fatalf("empty acquire: %v", err)
	}
	if got := pool.Stats().HygieneRoundTrips - baseline; got != 1 {
		t.Fatalf("hygiene round trips for one acquire = %d, want 1", got)
	}

	var result int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if result != 1 {
		t.Fatalf("QueryRow result = %d, want 1", result)
	}
	if got := pool.Stats().HygieneRoundTrips - baseline; got != 2 {
		t.Fatalf("hygiene round trips for two acquires = %d, want 2", got)
	}

	failuresBefore := pool.HygieneFailures()
	raw, err := pgx.Connect(ctx, db.URL)
	if err != nil {
		t.Fatalf("connect failing-hook probe: %v", err)
	}
	if err := raw.Close(ctx); err != nil {
		t.Fatalf("close failing-hook probe: %v", err)
	}
	// PrepareConn is deliberately not part of pgxadapter's public surface.
	// The test reaches the private hook only to provide a deterministic failed
	// hook input; the production pool remains opaque to callers.
	poolValue := reflect.ValueOf(pool).Elem().FieldByName("pool")
	inner := (*pgxpool.Pool)(unsafe.Pointer(poolValue.Pointer()))
	if ok, err := inner.Config().PrepareConn(ctx, raw); err != nil || ok {
		t.Fatalf("failing PrepareConn = (%v, %v), want (false, nil)", ok, err)
	}
	if got := pool.HygieneFailures(); got != failuresBefore+1 {
		t.Fatalf("HygieneFailures = %d after failed hook, want %d", got, failuresBefore+1)
	}
}

func TestTodo_PERFOPT_001_Security(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	role := "perfopt_001_role"
	if _, err := db.Conn.Exec(ctx, "CREATE ROLE "+role); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Conn.Exec(context.Background(), "DROP ROLE "+role)
	})

	pool, err := pgxadapter.NewPool(ctx, db.URL+"&pool_max_conns=1", map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	const lockKey = int64(829400101)
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		for _, statement := range []string{
			"SET ROLE " + role,
			fmt.Sprintf("SELECT pg_advisory_lock(%d)", lockKey),
			"CREATE TEMP TABLE perfopt_leaked_temp(value text)",
			"PREPARE perfopt_leaked_stmt AS SELECT 1",
			"SET hcmnext.perfopt_setting = 'secret'",
		} {
			if _, err := conn.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("mutate pooled session: %v", err)
	}

	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		var sessionUser, currentUser string
		if err := conn.QueryRow(ctx, "SELECT session_user, current_user").Scan(&sessionUser, &currentUser); err != nil {
			return err
		}
		if currentUser != sessionUser || currentUser == role {
			return fmt.Errorf("role leaked: session_user=%q current_user=%q", sessionUser, currentUser)
		}

		var locks int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND objid = $1", lockKey).Scan(&locks); err != nil {
			return err
		}
		if locks != 0 {
			return fmt.Errorf("advisory locks leaked: %d", locks)
		}

		var setting string
		if err := conn.QueryRow(ctx, "SELECT coalesce(current_setting('hcmnext.perfopt_setting', true), '')").Scan(&setting); err != nil {
			return err
		}
		if setting != "" {
			return fmt.Errorf("custom setting leaked: %q", setting)
		}

		var count int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM perfopt_leaked_temp").Scan(&count); err == nil {
			return errors.New("temporary table leaked")
		}
		var prepared int
		if err := conn.QueryRow(ctx, "EXECUTE perfopt_leaked_stmt").Scan(&prepared); err == nil {
			return errors.New("prepared statement leaked")
		}
		return nil
	}); err != nil {
		t.Fatalf("verify session cleanup: %v", err)
	}
}

func TestTodo_PERFOPT_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()

	config, err := pgx.ParseConfig(db.URL)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	config.RuntimeParams["search_path"] = db.Schema
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("ConnectConfig: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, "DISCARD ALL", pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("DISCARD ALL startup-parameter probe: %v", err)
	}
	var searchPath string
	if err := conn.QueryRow(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("SHOW search_path after DISCARD ALL: %v", err)
	}
	if searchPath != db.Schema {
		t.Fatalf("startup search_path after DISCARD ALL = %q, want %q", searchPath, db.Schema)
	}

	pool, err := pgxadapter.NewPool(ctx, db.URL+"&pool_max_conns=1", map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	for attempt := 0; attempt < 12; attempt++ {
		if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
			var got string
			if err := conn.QueryRow(ctx, "SHOW search_path").Scan(&got); err != nil {
				return err
			}
			if got != db.Schema {
				return fmt.Errorf("acquire %d search_path = %q, want %q", attempt, got, db.Schema)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_PERFOPT_001_StartupParameterEvidence(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	config, err := pgx.ParseConfig(db.URL)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	config.RuntimeParams["search_path"] = db.Schema
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("ConnectConfig: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, "DISCARD ALL", pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("DISCARD ALL: %v", err)
	}
	var got string
	if err := conn.QueryRow(ctx, "SHOW search_path").Scan(&got); err != nil {
		t.Fatalf("SHOW search_path: %v", err)
	}
	if strings.TrimSpace(got) != db.Schema {
		t.Fatalf("startup parameter did not survive DISCARD ALL: got %q, want %q", got, db.Schema)
	}
}
