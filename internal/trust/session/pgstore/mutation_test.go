package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/session"
	"github.com/monstercameron/hcm-next/internal/trust/session/pgstore"
)

// TestTodo_SECARCH_002_Mutation proves the two mechanisms
// [pgstore.PGStore.Refresh] depends on -- trust_session_refresh_generation's
// UNIQUE(token_hash) constraint, and trust_session's version compare-and-
// swap -- are load-bearing, not decorative: with either one removed (or, for
// the CAS predicate, simply omitted from a statement), a write the real
// schema refuses is instead admitted.
func TestTodo_SECARCH_002_Mutation(t *testing.T) {
	t.Parallel()

	t.Run("UNIQUE(token_hash) is what refuses a re-issued token hash", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		// The real schema refuses re-inserting an already-issued token
		// hash under a new generation of the same session -- exactly the
		// row a replay-shaped double rotation would try to write.
		real := pgtest.New(t)
		tenant := newTenantID()
		insertTenant(t, real, tenant)
		realConn := appConn(t, real)
		rec, _, err := createSession(ctx, t, realConn, tenant)
		if err != nil {
			t.Fatalf("create fixture session: %v", err)
		}
		if err := insertDuplicateGeneration(ctx, realConn, tenant, rec); err == nil {
			t.Fatal("real schema: re-inserting an already-issued token hash under a new generation was admitted, want it refused by UNIQUE(token_hash)")
		}

		// The identical fixture, replayed against a schema with that one
		// constraint dropped, is admitted -- proving the constraint above,
		// not something else (a NOT NULL check, a foreign key, a
		// different unique index), is what stood in the way.
		mutant := pgtest.New(t)
		mutant.Exec(t, `ALTER TABLE trust_session_refresh_generation DROP CONSTRAINT trust_session_refresh_generation_hash_unique`)
		insertTenant(t, mutant, tenant)
		mutantConn := appConn(t, mutant)
		mutantRec, _, err := createSession(ctx, t, mutantConn, tenant)
		if err != nil {
			t.Fatalf("create fixture session on mutant schema: %v", err)
		}
		if err := insertDuplicateGeneration(ctx, mutantConn, tenant, mutantRec); err != nil {
			t.Fatalf("mutant schema (UNIQUE(token_hash) dropped): re-inserting an already-issued token hash was still refused (%v); the constraint is not what this test isolates", err)
		}
	})

	t.Run("the version compare-and-swap is what refuses a stale write", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		db := pgtest.New(t)
		tenant := newTenantID()
		insertTenant(t, db, tenant)

		mgr, err := session.NewPersistentManager(session.PersistentManagerConfig{
			Now: func() time.Time { return fixedInstant }, Store: pgstore.New(appConn(t, db)),
		})
		if err != nil {
			t.Fatalf("NewPersistentManager: %v", err)
		}
		rec, tok0, err := mgr.Create(ctx, session.CreateSpec{
			Tenant: tenant, Subject: "user:cas-mutation", PrincipalFingerprint: "fp:cas",
			Assurance: trust.AssuranceSubstantial,
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, _, err := mgr.Refresh(ctx, tok0); err != nil {
			t.Fatalf("Refresh (advance past generation 0): %v", err)
		}

		conn := appConn(t, db)

		// A write guarded by the version this row actually had BEFORE the
		// refresh above (now stale) is refused: zero rows affected. This
		// is the exact shape a CAS predicate silently dropped from a
		// statement's WHERE clause would stop protecting against.
		affected := casUpdate(ctx, t, conn, tenant, rec.ID())
		if affected != 0 {
			t.Fatalf("UPDATE guarded by a stale version affected %d row(s), want 0 -- the compare-and-swap is not enforced", affected)
		}

		// The identical statement with the version predicate removed
		// entirely (the mutant this sub-test names) DOES affect the row --
		// proving the predicate above, not something else, made the
		// difference.
		affected = unguardedUpdate(ctx, t, conn, tenant, rec.ID())
		if affected != 1 {
			t.Fatalf("control UPDATE with no version predicate affected %d row(s), want exactly 1", affected)
		}
	})
}

// createSession opens a [session.PersistentManager] over conn and creates
// one active session for tenant, satisfying every foreign key
// insertDuplicateGeneration and the CAS helpers below depend on
// (trust_session_pointer and trust_session rows, plus the real generation-0
// refresh row).
func createSession(ctx context.Context, t *testing.T, conn *pgxadapter.Conn, tenant values.TenantId) (session.Record, session.RefreshToken, error) {
	t.Helper()
	mgr, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: func() time.Time { return fixedInstant }, Store: pgstore.New(conn),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	return mgr.Create(ctx, session.CreateSpec{
		Tenant: tenant, Subject: "user:mutation-fixture", PrincipalFingerprint: "fp:mutation",
		Assurance: trust.AssuranceHigh,
	})
}

// insertDuplicateGeneration attempts to record rec's own already-issued
// current token hash a second time, under a generation number that has
// never been used ((tenant, session, generation) is fresh, so only
// UNIQUE(token_hash) -- never the table's primary key -- can refuse this).
func insertDuplicateGeneration(ctx context.Context, conn *pgxadapter.Conn, tenant values.TenantId, rec session.Record) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
		return err
	}
	var currentHash string
	if err := tx.QueryRow(ctx, `SELECT current_token_hash FROM trust_session WHERE tenant_id = $1::uuid AND session_id = $2`,
		tenant.String(), string(rec.ID())).Scan(&currentHash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO trust_session_refresh_generation (tenant_id, session_id, generation, token_hash)
		VALUES ($1::uuid, $2, 999, $3)`, tenant.String(), string(rec.ID()), currentHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// casUpdate runs the exact statement [pgstore.PGStore.Touch] would run to
// bump last_activity_at, guarded by a version deliberately one below the
// row's real current version (id has been rotated once since creation, so
// its real version is at least 2; 1 is therefore always stale), and reports
// how many rows it affected.
func casUpdate(ctx context.Context, t *testing.T, conn *pgxadapter.Conn, tenant values.TenantId, id session.ID) int64 {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	affected, err := tx.Exec(ctx, `
		UPDATE trust_session SET last_activity_at = now(), version = version + 1
		WHERE tenant_id = $1::uuid AND session_id = $2 AND version = 1`, tenant.String(), string(id))
	if err != nil {
		t.Fatalf("guarded update: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return affected
}

// unguardedUpdate is casUpdate's control: the identical statement with no
// version predicate at all.
func unguardedUpdate(ctx context.Context, t *testing.T, conn *pgxadapter.Conn, tenant values.TenantId, id session.ID) int64 {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	affected, err := tx.Exec(ctx, `
		UPDATE trust_session SET last_activity_at = now()
		WHERE tenant_id = $1::uuid AND session_id = $2`, tenant.String(), string(id))
	if err != nil {
		t.Fatalf("unguarded update: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return affected
}
