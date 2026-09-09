package pgstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session/pgstore"
)

// TestTodo_SECARCH_002_Security covers the two adversarial cases the todo's
// own SECURITY clause names: a cross-tenant read of trust_session must see
// nothing under row level security on a real PostgreSQL connection (not a
// unit-tested predicate; the least-privilege hcmnext_app role against the
// actual policy migration 00041 installs), and presenting a refresh token
// from an OLDER generation than the session's current one -- not just the
// immediately-previous one -- revokes the whole family, matching
// [session.Manager]'s own documented replay contract ("presenting any
// retired token ... is a replay").
func TestTodo_SECARCH_002_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantA := newTenantID()
	tenantB := newTenantID()
	insertTenant(t, db, tenantA)
	insertTenant(t, db, tenantB)

	mgr, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: func() time.Time { return fixedInstant }, Store: pgstore.New(appConn(t, db)),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}

	recA, _, err := mgr.Create(ctx, session.CreateSpec{
		Tenant: tenantA, Subject: "user:tenant-a", PrincipalFingerprint: "fp:a",
		Assurance: trust.AssuranceSubstantial,
	})
	if err != nil {
		t.Fatalf("Create (tenant A): %v", err)
	}

	// A real connection, scoped to tenant B via the exact same
	// internal/data/tenancy.WithTenant path production code uses, querying
	// trust_session directly (bypassing this package's own Go-level
	// tenant-parameter plumbing entirely) must see zero rows for tenant A's
	// session -- row level security itself is what refuses this, not any
	// application-level filter.
	crossConn := appConn(t, db)
	var count int
	txErr := inTenantTxErr(crossConn, tenantB, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM trust_session WHERE session_id = $1`, string(recA.ID()),
		).Scan(&count)
	})
	if txErr != nil {
		t.Fatalf("cross-tenant read under RLS: %v", txErr)
	}
	if count != 0 {
		t.Fatalf("tenant B saw %d row(s) for tenant A's session under row level security, want 0", count)
	}

	// The same is true of the evidence trail.
	var evCount int
	txErr = inTenantTxErr(crossConn, tenantB, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM trust_session_evidence WHERE session_id = $1`, string(recA.ID()),
		).Scan(&evCount)
	})
	if txErr != nil {
		t.Fatalf("cross-tenant evidence read under RLS: %v", txErr)
	}
	if evCount != 0 {
		t.Fatalf("tenant B saw %d evidence row(s) for tenant A's session under row level security, want 0", evCount)
	}

	// Owning tenant A still sees its own row, proving the zero count above
	// is RLS's tenant boundary and not a broken query.
	txErr = inTenantTxErr(crossConn, tenantA, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM trust_session WHERE session_id = $1`, string(recA.ID()),
		).Scan(&count)
	})
	if txErr != nil {
		t.Fatalf("same-tenant read: %v", txErr)
	}
	if count != 1 {
		t.Fatalf("tenant A saw %d row(s) for its own session, want 1", count)
	}

	// Replay of an OLDER generation, not merely the immediately-previous
	// one: rotate twice, then replay generation 0's token.
	_, tokGen0, err := mgr.Create(ctx, session.CreateSpec{
		Tenant: tenantA, Subject: "user:older-generation", PrincipalFingerprint: "fp:older",
		Assurance: trust.AssuranceHigh,
	})
	if err != nil {
		t.Fatalf("Create (older-generation session): %v", err)
	}
	rec1, tokGen1, err := mgr.Refresh(ctx, tokGen0)
	if err != nil {
		t.Fatalf("Refresh (gen0 -> gen1): %v", err)
	}
	rec2, _, err := mgr.Refresh(ctx, tokGen1)
	if err != nil {
		t.Fatalf("Refresh (gen1 -> gen2): %v", err)
	}
	if rec2.RotationCount() != 2 {
		t.Fatalf("rotation count = %d, want 2", rec2.RotationCount())
	}

	// tokGen0 is two generations stale, not one -- still a replay.
	if _, _, err := mgr.Refresh(ctx, tokGen0); !errors.Is(err, session.ErrRefreshReplay) {
		t.Fatalf("Refresh(generation-0 token, now two generations stale) error = %v, want ErrRefreshReplay", err)
	}
	after, err := mgr.Get(ctx, rec1.ID())
	if err != nil {
		t.Fatalf("Get after older-generation replay: %v", err)
	}
	if after.Status() != session.StatusRevoked || after.RevokedReason() != session.ReasonRefreshReplay {
		t.Fatalf("after older-generation replay: status=%s reason=%q, want REVOKED/refresh_replay_detected",
			after.Status(), after.RevokedReason())
	}
}

// inTenantTxErr runs fn inside its own transaction on conn, scoped to
// tenant by setting migration 00008/00041's app.tenant_id session setting
// directly (tenant is a [values.TenantId] carrying the storage uuid as its
// own string form, not a github.com/google/uuid.UUID, so
// [tenancy.WithTenant] itself does not apply here -- see
// internal/authn/issuerregistry/pgstore.go's withTenant for the identical
// choice), and commits it.
func inTenantTxErr(conn *pgxadapter.Conn, tenant values.TenantId, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, tenancy.SessionSetting, tenant.String()); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
