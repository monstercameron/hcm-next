package uow_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/uow"
)

// TestTodo_DB_018_Integration runs [uow.RunConformance] against both shipped
// [uow.AggregateRepository] implementations -- [uow.PostgresWorkerRepository]
// over a real pgtest-backed worker table, and [uow.MemoryRepository] with no
// database at all -- and separately proves a single [uow.UnitOfWork] commits
// every registered aggregate (here, two distinct Worker entities) together in
// one transaction.
func TestTodo_DB_018_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("conformance: PostgresWorkerRepository", func(t *testing.T) {
		t.Parallel()
		tenant := insertTenant(t, db, "db018-conf-pg")
		personRef := newPersonRef(t, db, tenant)
		conn := appConn(t, db, tenant)
		uow.RunConformance(t, uow.ConformanceCase[aggregates.Worker]{
			Repo:   uow.PostgresWorkerRepository{},
			Exec:   conn,
			Tenant: tenant,
			Build:  workerBuilder(t, tenant, personRef),
		})
	})

	t.Run("conformance: MemoryRepository", func(t *testing.T) {
		t.Parallel()
		uow.RunConformance(t, uow.ConformanceCase[string]{
			Repo:   uow.NewMemoryRepository[string](),
			Exec:   nil,
			Tenant: uuid.New(),
			Build: func(entityID uuid.UUID, seq int, businessAt time.Time) string {
				return entityID.String()[:8]
			},
		})
	})

	t.Run("one UnitOfWork commits two distinct aggregates together", func(t *testing.T) {
		tenant := insertTenant(t, db, "db018-multi")
		personRef := newPersonRef(t, db, tenant)
		build := workerBuilder(t, tenant, personRef)
		repo := uow.PostgresWorkerRepository{}
		conn := appConn(t, db, tenant)

		entityA, entityB := uuid.New(), uuid.New()
		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u, "worker", entityA, build(entityA, 1, fixedInstant), 0); err != nil {
			t.Fatalf("Stage entity A: %v", err)
		}
		if err := uow.Stage(u, "worker", entityB, build(entityB, 1, fixedInstant), 0); err != nil {
			t.Fatalf("Stage entity B: %v", err)
		}
		if err := u.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		for _, id := range []uuid.UUID{entityA, entityB} {
			_, version, err := repo.Load(ctx, conn, tenant, id, fixedInstant)
			if err != nil {
				t.Fatalf("Load %s: %v", id, err)
			}
			if version != 1 {
				t.Fatalf("Load %s version = %d, want 1", id, version)
			}
		}

		// A second unit of work advances both together at once more, proving
		// the "every registered aggregate's version checked" language is not
		// limited to a single aggregate per unit of work.
		u2 := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u2, "worker", entityA, build(entityA, 2, fixedInstant), 1); err != nil {
			t.Fatalf("Stage entity A (rev 2): %v", err)
		}
		if err := uow.Stage(u2, "worker", entityB, build(entityB, 2, fixedInstant), 1); err != nil {
			t.Fatalf("Stage entity B (rev 2): %v", err)
		}
		if err := u2.Commit(ctx); err != nil {
			t.Fatalf("second Commit: %v", err)
		}
		for _, id := range []uuid.UUID{entityA, entityB} {
			revs, err := repo.ListRevisions(ctx, conn, tenant, id)
			if err != nil {
				t.Fatalf("ListRevisions %s: %v", id, err)
			}
			if len(revs) != 2 {
				t.Fatalf("ListRevisions %s = %d rows, want 2", id, len(revs))
			}
		}
	})
}
