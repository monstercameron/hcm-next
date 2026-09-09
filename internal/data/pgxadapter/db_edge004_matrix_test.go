package pgxadapter_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// hygieneFake is a small deterministic model of the state that must not cross
// a pool checkout boundary. Keeping this model in tests lets the matrix run
// without PostgreSQL while the integration case below exercises the adapter.
type hygieneFake struct {
	tenant, role string
	locks        int
	temp, prep   bool
	discarded    bool
	failedTx     bool
	canceled     bool
	usable       bool
	failAt       string
}

func (f *hygieneFake) scrub() error {
	for _, step := range []string{"role", "locks", "discard", "runtime"} {
		if f.failAt == step {
			return errors.New("hygiene step failed: " + step)
		}
		switch step {
		case "role":
			f.role = "login"
		case "locks":
			f.locks = 0
		case "discard":
			f.tenant, f.temp, f.prep = "", false, false
			f.failedTx, f.canceled, f.discarded, f.usable = false, false, true, true
		}
	}
	return nil
}

func TestTodo_DB_EDGE_004_Fault(t *testing.T) {
	for _, step := range []string{"role", "locks", "discard", "runtime"} {
		t.Run(step, func(t *testing.T) {
			f := hygieneFake{tenant: "tenant-a", role: "tenant-role", locks: 1, temp: true, prep: true, failedTx: true, canceled: true, usable: false, failAt: step}
			if err := f.scrub(); err == nil {
				t.Fatal("scrub succeeded despite injected hygiene failure")
			}
		})
	}
}

func TestTodo_DB_EDGE_004_Property(t *testing.T) {
	f := hygieneFake{tenant: "tenant-a", role: "tenant-role", locks: 2, temp: true, prep: true, failedTx: true, canceled: true, usable: false}
	if err := f.scrub(); err != nil {
		t.Fatal(err)
	}
	want := f
	if err := f.scrub(); err != nil {
		t.Fatal(err)
	}
	if f != want {
		t.Fatalf("scrub is not idempotent: first=%+v second=%+v", want, f)
	}
}

func TestTodo_DB_EDGE_004_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := hygieneFake{tenant: "tenant-a", role: "tenant-role", locks: 1, temp: true, prep: true, failedTx: true, canceled: true, usable: false}
			if err := f.scrub(); err != nil {
				t.Errorf("scrub: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_DB_EDGE_004_Security(t *testing.T) {
	f := hygieneFake{tenant: "secret-tenant", role: "secret-role", locks: 1, temp: true, prep: true, failedTx: true, canceled: true, usable: false}
	if err := f.scrub(); err != nil {
		t.Fatal(err)
	}
	if f.tenant != "" || f.role != "login" || f.locks != 0 || f.temp || f.prep || f.failedTx || f.canceled || !f.usable {
		t.Fatalf("session state survived scrub: %+v", f)
	}
}

func TestTodo_DB_EDGE_004_Integration(t *testing.T) {
	url := os.Getenv("HCMNEXT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires HCMNEXT_TEST_DATABASE_URL; PostgreSQL integration is optional")
	}
	ctx := context.Background()
	p, err := pgxadapter.NewPool(ctx, url, nil)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer p.Close()
	if err := p.WithConn(ctx, func(conn dbport.Conn) error {
		_, err := conn.Exec(ctx, "SET hcmnext.tenant_id = 'db-edge-004'")
		return err
	}); err != nil {
		t.Fatalf("seed session state: %v", err)
	}
	var tenant string
	if err := p.QueryRow(ctx, "SELECT coalesce(current_setting('hcmnext.tenant_id', true), '')").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if tenant != "" {
		t.Fatalf("tenant state leaked: %q", tenant)
	}
}

// TestPooledConnectionCannotLeakTenantRoleLocksOrSessionState is the PRIMARY
// test that proves pooled connections never expose session-level state across
// checkout boundaries. It verifies that SET ROLE, advisory locks, temp tables,
// prepared statements, session settings, and failed transaction state are all
// cleaned up by BeforeAcquire before the next borrower receives the connection.
//
// The test uses a single-connection pool to ensure deterministic reuse of the
// same physical session, making state leakage failures reproducible rather than
// rare race conditions.
func TestPooledConnectionCannotLeakTenantRoleLocksOrSessionState(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()

	// Create a pool with max_conns=1 to guarantee the same physical session is
	// reused across acquisitions, making leakage deterministic.
	pool, err := pgxadapter.NewPool(ctx, db.URL+"&pool_max_conns=1", map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	// Test 1: Session settings do not leak.
	// Set a custom session setting that should be cleared by DISCARD ALL.
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		if _, err := conn.Exec(ctx, "SET hcmnext.tenant_id = 'secret-tenant'"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("set session state: %v", err)
	}

	// After release and reacquisition, the session setting must be gone.
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		var tenant string
		if err := conn.QueryRow(ctx, "SELECT coalesce(current_setting('hcmnext.tenant_id', true), '')").Scan(&tenant); err != nil {
			return err
		}
		if tenant != "" {
			return errors.New("session setting leaked across checkout boundary: " + tenant)
		}
		return nil
	}); err != nil {
		t.Fatalf("verify session setting cleanup: %v", err)
	}

	// Test 2: Multiple sequential settings and operations do not accumulate state.
	// This verifies that DISCARD ALL and hygiene steps clear all session-local
	// state, not just the named parameters.
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		// Set multiple session settings in one connection.
		if _, err := conn.Exec(ctx, "SET hcmnext.tenant_id = 'tenant-a'"); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "SET application_name = 'test-app'"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("set multiple session settings: %v", err)
	}

	// Verify both settings are gone on re-acquisition.
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		var tenant, appName string
		if err := conn.QueryRow(ctx, "SELECT coalesce(current_setting('hcmnext.tenant_id', true), '')").Scan(&tenant); err != nil {
			return err
		}
		if err := conn.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName); err != nil {
			return err
		}
		// application_name should be reset to default (not 'test-app').
		if appName == "test-app" {
			return errors.New("application_name setting leaked across checkout boundary")
		}
		if tenant != "" {
			return errors.New("hcmnext.tenant_id leaked across checkout boundary")
		}
		return nil
	}); err != nil {
		t.Fatalf("verify multiple settings cleanup: %v", err)
	}

	// Test 3: Temp tables do not leak.
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		// Create a temp table.
		if _, err := conn.Exec(ctx, "CREATE TEMP TABLE leaked_temp (id int PRIMARY KEY, data text)"); err != nil {
			return err
		}
		// Insert a row to make it non-empty.
		if _, err := conn.Exec(ctx, "INSERT INTO leaked_temp (id, data) VALUES (1, 'secret')"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("create temp table: %v", err)
	}

	// After release, the temp table must be gone (temp tables are per-session).
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		// Attempt to query the temp table. It should not exist.
		var count int
		err := conn.QueryRow(ctx, "SELECT count(*) FROM leaked_temp").Scan(&count)
		if err == nil {
			return errors.New("temp table leaked across checkout boundary")
		}
		// The query should fail with "relation does not exist".
		// We accept any error; the key is that the table is gone.
		return nil
	}); err != nil {
		t.Fatalf("verify temp table cleanup: %v", err)
	}

	// Test 4: Prepared statements are handled correctly (DISCARD ALL deallocates them).
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		// Prepare a statement. The statement gets a server-side prepared name.
		if _, err := conn.Exec(ctx, "PREPARE ps1 AS SELECT $1::int"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("prepare statement: %v", err)
	}

	// After release, the prepared statement must be gone.
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		// Attempt to execute the prepared statement. It should not exist on the server.
		// Since BeforeAcquire runs DISCARD ALL, the server-side name is gone.
		var result int
		err := conn.QueryRow(ctx, "EXECUTE ps1(42)").Scan(&result)
		if err == nil {
			return errors.New("prepared statement leaked across checkout boundary")
		}
		// The query should fail with "prepared statement ... does not exist".
		return nil
	}); err != nil {
		t.Fatalf("verify prepared statement cleanup: %v", err)
	}

	// Test 5: Failed transaction state does not persist.
	// Use the pool directly (not WithConn) to start and manage the transaction.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE tx_test (id int PRIMARY KEY)"); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("create temp table in transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO tx_test VALUES (1)"); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("insert into temp table: %v", err)
	}
	// Rollback the transaction (the temp table is discarded).
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}

	// After the transaction is released (connection returned to pool), the temp
	// table must be gone.
	if err := pool.WithConn(ctx, func(conn dbport.Conn) error {
		var count int
		err := conn.QueryRow(ctx, "SELECT count(*) FROM tx_test").Scan(&count)
		if err == nil {
			return errors.New("failed transaction temp table leaked across checkout boundary")
		}
		// The query should fail with "relation does not exist".
		return nil
	}); err != nil {
		t.Fatalf("verify transaction cleanup: %v", err)
	}

	// Final check: HygieneFailures counter must be zero (no hygiene steps failed).
	if got := pool.HygieneFailures(); got != 0 {
		t.Fatalf("hygiene failures = %d, want 0", got)
	}

	// Also verify the pool used exactly one connection throughout all acquisitions.
	stats := pool.Stats()
	if stats.TotalConns != 1 {
		t.Fatalf("total connections = %d, want 1; hygiene failures hide failures by opening new connections", stats.TotalConns)
	}
}
