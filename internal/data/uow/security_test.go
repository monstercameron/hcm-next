package uow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/uow"
)

// TestTodo_DB_018_Security proves a UnitOfWork's tenant binding is what it
// claims to be: [uow.Begin] refuses the nil tenant outright (delegating to
// internal/data/tenancy's own fail-closed guard, never silently scoping to
// the all-zero tenant), and a unit of work bound to one tenant cannot read or
// write another tenant's worker rows even when it names that other tenant's
// own real entity id. Two independent layers each refuse this on their own:
// [uow.PostgresWorkerRepository.Save] itself refuses an aggregate whose own
// Tenant field disagrees with the unit of work's tenant before it issues any
// SQL, and -- for a call that named a tenant matching neither the connection's
// session scope nor the aggregate, such as Load below -- migration 00008's
// row level security policy on the worker table, enforced for the
// least-privilege hcmnext_app role, confines what even reaches the query
// planner. Neither is "merely an application-level tenant_id filter this
// package could forget to apply".
func TestTodo_DB_018_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	repo := uow.PostgresWorkerRepository{}

	tenantA := insertTenant(t, db, "db018-sec-a")
	tenantB := insertTenant(t, db, "db018-sec-b")
	personRefA := newPersonRef(t, db, tenantA)
	personRefB := newPersonRef(t, db, tenantB)
	buildA := workerBuilder(t, tenantA, personRefA)
	buildB := workerBuilder(t, tenantB, personRefB)

	connA := appConn(t, db, tenantA)
	entityB := uuid.New()
	// Seed tenant B's worker directly (its own connection, its own tenant
	// scope), entirely independent of tenant A's unit of work below.
	connB := appConn(t, db, tenantB)
	uB := beginWorkerUnit(t, ctx, connB, tenantB, repo)
	if err := uow.Stage(uB, "worker", entityB, buildB(entityB, 1, fixedInstant), 0); err != nil {
		t.Fatalf("seed tenant B worker: Stage: %v", err)
	}
	if err := uB.Commit(ctx); err != nil {
		t.Fatalf("seed tenant B worker: Commit: %v", err)
	}

	t.Run("Begin refuses the nil tenant", func(t *testing.T) {
		if _, err := uow.Begin(ctx, connA, uuid.Nil); err == nil {
			t.Fatal("uow.Begin accepted the nil tenant")
		}
	})

	t.Run("a unit of work bound to tenant A cannot Load tenant B's worker by its real entity id", func(t *testing.T) {
		u := beginWorkerUnit(t, ctx, connA, tenantA, repo)
		defer func() { _ = u.Rollback(ctx) }()
		_, _, err := repo.Load(ctx, u.Tx(), tenantB, entityB, fixedInstant)
		if err == nil {
			t.Fatal("Load under tenant A's scope returned tenant B's worker; row level security did not confine it")
		}
		if !errors.Is(err, aggregates.ErrNotFound) {
			t.Fatalf("Load across tenants = %v, want aggregates.ErrNotFound (empty, not a distinguishing error)", err)
		}
	})

	t.Run("a unit of work bound to tenant A cannot supersede tenant B's live worker row", func(t *testing.T) {
		entityA := uuid.New()
		seedA := beginWorkerUnit(t, ctx, connA, tenantA, repo)
		if err := uow.Stage(seedA, "worker", entityA, buildA(entityA, 1, fixedInstant), 0); err != nil {
			t.Fatalf("seed tenant A worker: %v", err)
		}
		if err := seedA.Commit(ctx); err != nil {
			t.Fatalf("seed tenant A worker: Commit: %v", err)
		}

		u := beginWorkerUnit(t, ctx, connA, tenantA, repo)
		// entityB is tenant B's worker, at its real version 1; a save under
		// tenant A's scope must not be able to touch it at all.
		if err := uow.Stage(u, "worker", entityB, buildB(entityB, 2, fixedInstant), 1); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if err := u.Commit(ctx); err == nil {
			t.Fatal("a unit of work scoped to tenant A superseded tenant B's worker row")
		}

		// Tenant B's own view is unaffected.
		_, versionB, err := repo.Load(ctx, connB, tenantB, entityB, fixedInstant)
		if err != nil {
			t.Fatalf("Load tenant B worker after the cross-tenant attempt: %v", err)
		}
		if versionB != 1 {
			t.Fatalf("tenant B worker version = %d after a cross-tenant write attempt, want unchanged 1", versionB)
		}
	})

	t.Run("the app role carries no bypass of row level security", func(t *testing.T) {
		var rolbypassrls bool
		if err := db.Conn.QueryRow(ctx, `SELECT rolbypassrls FROM pg_roles WHERE rolname = 'hcmnext_app'`).Scan(&rolbypassrls); err != nil {
			t.Fatalf("read pg_roles: %v", err)
		}
		if rolbypassrls {
			t.Fatal("hcmnext_app carries BYPASSRLS; the tenant scope this package relies on would not confine anything")
		}
	})
}
