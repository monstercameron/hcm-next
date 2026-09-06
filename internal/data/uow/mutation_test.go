package uow_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/uow"
)

// TestTodo_DB_018_Mutation proves that every refusal this package can produce
// -- a stale Save inside a single Stage/Commit, and a Commit that refuses one
// of several staged aggregates -- mutates nothing, and that a raw UPDATE
// against the worker table bypassing this package entirely is still refused
// by migrations/00011's aggregate_forbid_inplace_update trigger, the
// database-level backstop this package's own compare-and-swap logic sits on
// top of rather than replaces.
func TestTodo_DB_018_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	repo := uow.PostgresWorkerRepository{}

	t.Run("a Commit that fails one of several staged aggregates leaves none of them partially applied", func(t *testing.T) {
		tenant := insertTenant(t, db, "db018-mut-fault")
		personRef := newPersonRef(t, db, tenant)
		build := workerBuilder(t, tenant, personRef)
		conn := appConn(t, db, tenant)

		good, bad := uuid.New(), uuid.New()
		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u, "worker", good, build(good, 1, fixedInstant), 0); err != nil {
			t.Fatalf("Stage good: %v", err)
		}
		// bad is staged at expectedVersion 1, but it has never been created
		// (its true version is 0): the second staged write is the one that
		// fails, after the first has already run its statements against the
		// same, still-open transaction.
		if err := uow.Stage(u, "worker", bad, build(bad, 2, fixedInstant), 1); err != nil {
			t.Fatalf("Stage bad: %v", err)
		}
		if err := u.Commit(ctx); err == nil {
			t.Fatal("Commit with one stale staged aggregate succeeded")
		}

		if _, _, err := repo.Load(ctx, conn, tenant, good, fixedInstant); err == nil {
			t.Fatal("the FIRST staged aggregate (good) was committed despite the SECOND one's refusal; Commit is not atomic")
		}
		revs, err := repo.ListRevisions(ctx, conn, tenant, good)
		if err != nil {
			t.Fatalf("ListRevisions good: %v", err)
		}
		if len(revs) != 0 {
			t.Fatalf("ListRevisions good after a refused Commit = %d rows, want 0 (rolled back)", len(revs))
		}
		badRevs, err := repo.ListRevisions(ctx, conn, tenant, bad)
		if err != nil {
			t.Fatalf("ListRevisions bad: %v", err)
		}
		if len(badRevs) != 0 {
			t.Fatalf("ListRevisions bad after a refused Commit = %d rows, want 0", len(badRevs))
		}
	})

	t.Run("a refused single-aggregate Save leaves the live revision exactly as it was", func(t *testing.T) {
		tenant := insertTenant(t, db, "db018-mut-single")
		personRef := newPersonRef(t, db, tenant)
		build := workerBuilder(t, tenant, personRef)
		conn := appConn(t, db, tenant)
		entityID := uuid.New()

		u1 := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u1, "worker", entityID, build(entityID, 1, fixedInstant), 0); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if err := u1.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		before, _, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		u2 := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u2, "worker", entityID, build(entityID, 2, fixedInstant), 0); err != nil { // stale: real version is 1
			t.Fatalf("Stage: %v", err)
		}
		if err := u2.Commit(ctx); err == nil {
			t.Fatal("Commit at a stale expectedVersion succeeded")
		}

		after, version, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load after the refusal: %v", err)
		}
		if version != 1 {
			t.Fatalf("version after a refused Save = %d, want unchanged 1", version)
		}
		if after.RowID != before.RowID || after.Digest != before.Digest {
			t.Fatalf("the live revision changed identity after a refused Save: %+v -> %+v", before, after)
		}
	})

	t.Run("RED: a raw UPDATE against the worker table's business columns is refused, bypassing this package entirely", func(t *testing.T) {
		tenant := insertTenant(t, db, "db018-mut-raw")
		personRef := newPersonRef(t, db, tenant)
		build := workerBuilder(t, tenant, personRef)
		conn := appConn(t, db, tenant)
		entityID := uuid.New()

		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u, "worker", entityID, build(entityID, 1, fixedInstant), 0); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if err := u.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		err := db.ExecErr(`UPDATE worker SET lifecycle_status = 'TERMINATED' WHERE entity_id = $1 AND superseded_at IS NULL`, entityID)
		if err == nil {
			t.Fatal("a raw UPDATE of worker.lifecycle_status was accepted; the append-only trigger should have refused it")
		}

		unchanged, _, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load after the refused raw UPDATE: %v", err)
		}
		if unchanged.LifecycleStatus != "PENDING" {
			t.Fatalf("LifecycleStatus after a refused raw UPDATE = %q, want unchanged PENDING", unchanged.LifecycleStatus)
		}
	})
}
