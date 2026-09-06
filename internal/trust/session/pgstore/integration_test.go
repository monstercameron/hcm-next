package pgstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/session"
	"github.com/monstercameron/hcm-next/internal/trust/session/pgstore"
)

// TestTodo_SECARCH_002_Integration is the two-replica proof SECARCH-002's
// GREEN clause asks for: two independent [session.PersistentManager]
// instances, each with its own [pgstore.PGStore] and its own connection,
// sharing exactly one PostgreSQL database. A session created on one is
// fully usable -- read, touched, rotated, revoked, and still subject to
// replay detection -- on the other, which is what "visible across every
// replica within a bounded propagation window" means for a synchronous SQL
// backing: the window is one commit.
func TestTodo_SECARCH_002_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := newTenantID()
	insertTenant(t, db, tenant)

	// Both replicas share the same (real, advancing) clock: a fixed,
	// per-replica clock would stamp events out of the actual order this
	// test performs them in, which is a test-fixture bug, not something
	// [pgstore.PGStore] needs to tolerate -- real replicas share one wall
	// clock (modulo ordinary skew), not two disagreeing frozen ones.
	replicaA, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: time.Now, Store: pgstore.New(appConn(t, db)),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager (replica A): %v", err)
	}
	replicaB, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: time.Now, Store: pgstore.New(appConn(t, db)),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager (replica B): %v", err)
	}

	rec, tok1, err := replicaA.Create(ctx, session.CreateSpec{
		Tenant: tenant, Subject: "user:integration", PrincipalFingerprint: "fp:integration",
		Assurance: trust.AssuranceSubstantial,
	})
	if err != nil {
		t.Fatalf("replica A Create: %v", err)
	}

	// Replica B, a brand new connection and a brand new in-process Manager,
	// nothing carried over from replica A: this is what a process restart
	// or a second API instance looks like from the database's point of
	// view (the same standard db012_integration_test.go's own restart test
	// uses).
	got, err := replicaB.Get(ctx, rec.ID())
	if err != nil {
		t.Fatalf("replica B Get: %v", err)
	}
	if got.ID() != rec.ID() || got.Tenant() != tenant || got.Subject() != "user:integration" || got.Status() != session.StatusActive {
		t.Fatalf("replica B loaded %+v, want replica A's own record %+v", got, rec)
	}

	if _, err := replicaB.Touch(ctx, rec.ID()); err != nil {
		t.Fatalf("replica B Touch: %v", err)
	}

	rotated, tok2, err := replicaB.Refresh(ctx, tok1)
	if err != nil {
		t.Fatalf("replica B Refresh: %v", err)
	}
	if rotated.RotationCount() != 1 {
		t.Fatalf("rotation count = %d, want 1", rotated.RotationCount())
	}

	// Replica A never performed that rotation itself, but must still see
	// the retired token as a replay -- the whole point of a shared durable
	// store instead of two independent in-memory ones.
	if _, _, err := replicaA.Refresh(ctx, tok1); !errors.Is(err, session.ErrRefreshReplay) {
		t.Fatalf("replica A Refresh(retired token) error = %v, want ErrRefreshReplay", err)
	}

	afterReplay, err := replicaB.Get(ctx, rec.ID())
	if err != nil {
		t.Fatalf("replica B Get after replay detected on replica A: %v", err)
	}
	if afterReplay.Status() != session.StatusRevoked || afterReplay.RevokedReason() != session.ReasonRefreshReplay {
		t.Fatalf("after cross-replica replay: status=%s reason=%q, want REVOKED/refresh_replay_detected",
			afterReplay.Status(), afterReplay.RevokedReason())
	}

	// The evidence trail is visible from either replica and carries every
	// transition, in order, regardless of which replica wrote each one.
	trail, err := replicaA.Evidence(ctx, rec.ID())
	if err != nil {
		t.Fatalf("replica A Evidence: %v", err)
	}
	wantKinds := []session.EvidenceKind{session.EvidenceCreated, session.EvidenceRotated, session.EvidenceRevoked}
	if len(trail) != len(wantKinds) {
		t.Fatalf("evidence trail = %+v, want %d entries", trail, len(wantKinds))
	}
	for i, want := range wantKinds {
		if trail[i].Kind != want {
			t.Fatalf("evidence[%d].Kind = %s, want %s (full trail: %+v)", i, trail[i].Kind, want, trail)
		}
	}

	// tok2 (the second generation) is also dead now: the family was
	// revoked, not just the replayed token.
	if _, _, err := replicaB.Refresh(ctx, tok2); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("Refresh with the current-but-now-revoked token error = %v, want ErrSessionNotActive", err)
	}
}

// TestTodo_SECARCH_002_RestartSurvives is the RED case restated literally:
// a session (and its evidence) created on one connection is fully readable
// from a brand new connection opened afterward, with nothing cached from
// the first -- what a process restart looks like from the database's own
// point of view.
func TestTodo_SECARCH_002_RestartSurvives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := newTenantID()
	insertTenant(t, db, tenant)

	before, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: func() time.Time { return fixedInstant }, Store: pgstore.New(appConn(t, db)),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager: %v", err)
	}
	rec, _, err := before.Create(ctx, session.CreateSpec{
		Tenant: tenant, Subject: "user:restart", PrincipalFingerprint: "fp:restart",
		Assurance: trust.AssuranceHigh,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The "restart": a brand new connection, a brand new Manager.
	after, err := session.NewPersistentManager(session.PersistentManagerConfig{
		Now: func() time.Time { return fixedInstant.Add(time.Minute) }, Store: pgstore.New(appConn(t, db)),
	})
	if err != nil {
		t.Fatalf("NewPersistentManager (post-restart): %v", err)
	}
	reloaded, err := after.Get(ctx, rec.ID())
	if err != nil {
		t.Fatalf("post-restart Get: %v", err)
	}
	if reloaded.Subject() != "user:restart" || reloaded.Status() != session.StatusActive {
		t.Fatalf("post-restart record = %+v, want the pre-restart session still active", reloaded)
	}
	if _, err := after.Revoke(ctx, rec.ID(), "post-restart-cleanup"); err != nil {
		t.Fatalf("post-restart Revoke: %v", err)
	}
}
