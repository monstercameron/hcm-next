package pgtest_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// TestPooledConnectionCannotLeakTenantRoleLocksOrSessionState proves that a
// physical session is scrubbed before it is handed to another borrower. The
// fixture deliberately mutates session-scoped settings, role, advisory locks,
// temporary objects and prepared statements, then verifies none survive.
func TestPooledConnectionCannotLeakTenantRoleLocksOrSessionState(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	p, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(p.Close)

	if err := p.WithConn(ctx, func(conn dbport.Conn) error {
		if _, err := conn.Exec(ctx, "SET hcmnext.tenant_id = 'tenant-a'"); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "SET ROLE postgres"); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(8294004)"); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, "CREATE TEMP TABLE leaked_temp(value text)"); err != nil {
			return err
		}
		_, err := conn.Exec(ctx, "PREPARE leaked_stmt AS SELECT 1")
		return err
	}); err != nil {
		t.Fatalf("mutate pooled session: %v", err)
	}

	// Use the ordinary pool query surface for the assertions. Each operation
	// acquires a freshly scrubbed session.
	var setting string
	if err := p.QueryRow(ctx, "SELECT coalesce(current_setting('hcmnext.tenant_id', true), '')").Scan(&setting); err != nil {
		t.Fatalf("read tenant setting: %v", err)
	}
	if setting != "" {
		t.Fatalf("tenant setting leaked as %q", setting)
	}
	var currentRole, loginRole string
	if err := p.QueryRow(ctx, "SELECT current_user, session_user").Scan(&currentRole, &loginRole); err != nil {
		t.Fatalf("read role: %v", err)
	}
	if currentRole != loginRole {
		t.Fatalf("role leaked as %q (login role %q)", currentRole, loginRole)
	}
	var existingLocks int
	if err := p.QueryRow(ctx, "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid()").Scan(&existingLocks); err != nil {
		t.Fatalf("probe existing advisory locks: %v", err)
	}
	if existingLocks != 0 {
		t.Fatalf("advisory locks leaked before probe: %d", existingLocks)
	}
	var locked bool
	if err := p.QueryRow(ctx, "SELECT pg_try_advisory_lock(8294004)").Scan(&locked); err != nil {
		t.Fatalf("probe advisory lock: %v", err)
	}
	if !locked {
		t.Fatal("advisory lock leaked to next borrower")
	}
	_, _ = p.Exec(ctx, "SELECT pg_advisory_unlock(8294004)")
	var tempExists bool
	if err := p.QueryRow(ctx, "SELECT to_regclass('pg_temp.leaked_temp') IS NOT NULL").Scan(&tempExists); err != nil {
		t.Fatalf("probe temp table: %v", err)
	}
	if tempExists {
		t.Fatal("temporary table leaked to next borrower")
	}
	var prepared int
	if err := p.QueryRow(ctx, "SELECT count(*) FROM pg_prepared_statements WHERE name = 'leaked_stmt'").Scan(&prepared); err != nil {
		t.Fatalf("probe prepared statement: %v", err)
	}
	if prepared != 0 {
		t.Fatal("prepared statement leaked to next borrower")
	}
}
