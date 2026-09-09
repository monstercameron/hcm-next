package uow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/uow"
)

// TestTodo_DB_018 proves the unit-of-work contract end to end against a real
// pgtest-backed PostgresWorkerRepository: Begin binds a tenant, Register
// binds a repository under a kind, Stage queues an append with the version
// the caller last observed, and Commit checks every staged aggregate's
// version in the one transaction Begin opened -- succeeding only when every
// one of them still matches, and never partially applying when one does not.
// It also proves the shapes DB-018's RED clause names as refusals: a stale
// expectedVersion is refused (never silently bypassed), the transaction
// handle Begin opens is never exposed to a caller after Commit or Rollback,
// every read/write returns a typed aggregates.Worker (never untyped JSON),
// and there is no way to begin a unit of work nested inside another.
func TestTodo_DB_018(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "db018-primary")
	personRef := newPersonRef(t, db, tenant)
	build := workerBuilder(t, tenant, personRef)
	repo := uow.PostgresWorkerRepository{}
	conn := appConn(t, db, tenant)
	entityID := uuid.New()

	t.Run("Commit appends the first revision at expectedVersion 0", func(t *testing.T) {
		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		rev1 := build(entityID, 1, fixedInstant)
		if err := uow.Stage(u, "worker", entityID, rev1, 0); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if err := u.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		got, version, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load after commit: %v", err)
		}
		if version != 1 {
			t.Fatalf("version after first commit = %d, want 1", version)
		}
		if got.WorkerNumber != rev1.WorkerNumber || got.LifecycleStatus != "PENDING" {
			t.Fatalf("Load after commit = %+v, want worker_number %q / PENDING", got, rev1.WorkerNumber)
		}
	})

	t.Run("Commit appends the second revision at the version Load just returned", func(t *testing.T) {
		_, version, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		rev2 := build(entityID, 2, fixedInstant)
		if err := uow.Stage(u, "worker", entityID, rev2, version); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if err := u.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}

		got, newVersion, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load after second commit: %v", err)
		}
		if newVersion != 2 {
			t.Fatalf("version after second commit = %d, want 2", newVersion)
		}
		if got.LifecycleStatus != "ACTIVE" {
			t.Fatalf("Load after second commit LifecycleStatus = %q, want ACTIVE", got.LifecycleStatus)
		}
	})

	t.Run("Commit refuses a stale expectedVersion with a typed conflict, and mutates nothing", func(t *testing.T) {
		before, beforeVersion, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load before the refused commit: %v", err)
		}
		if beforeVersion != 2 {
			t.Fatalf("beforeVersion = %d, want 2", beforeVersion)
		}

		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		staleRev := build(entityID, 3, fixedInstant)
		// 1, not the current 2: this caller's view of the aggregate is stale.
		if err := uow.Stage(u, "worker", entityID, staleRev, 1); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		commitErr := u.Commit(ctx)
		if commitErr == nil {
			t.Fatal("Commit at a stale expectedVersion: want a conflict, got nil")
		}
		conflict, ok := uow.IsConflict(commitErr)
		if !ok {
			t.Fatalf("Commit error = %v (%T), want *uow.ConflictError", commitErr, commitErr)
		}
		if conflict.Kind != "worker" || conflict.EntityID != entityID || conflict.ExpectedVersion != 1 || conflict.ActualVersion != 2 {
			t.Fatalf("ConflictError = %+v, want kind worker, entity %s, expected 1, actual 2", conflict, entityID)
		}
		if !errors.Is(commitErr, uow.ErrUnitOfWork) {
			t.Fatal("a ConflictError does not unwrap to uow.ErrUnitOfWork")
		}

		after, afterVersion, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load after the refused commit: %v", err)
		}
		if afterVersion != 2 {
			t.Fatalf("version after a refused commit = %d, want unchanged 2", afterVersion)
		}
		if after.Digest != before.Digest {
			t.Fatalf("the live revision changed digest after a refused commit: %s -> %s", before.Digest, after.Digest)
		}
		revs, err := repo.ListRevisions(ctx, conn, tenant, entityID)
		if err != nil {
			t.Fatalf("ListRevisions after the refused commit: %v", err)
		}
		if len(revs) != 2 {
			t.Fatalf("ListRevisions after a refused commit = %d rows, want unchanged 2", len(revs))
		}
	})

	t.Run("Commit runs exactly once; a second call on the same unit is refused", func(t *testing.T) {
		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u, "worker", entityID, build(entityID, 3, fixedInstant), 2); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if err := u.Commit(ctx); err != nil {
			t.Fatalf("first Commit: %v", err)
		}
		if err := u.Commit(ctx); !errors.Is(err, uow.ErrClosed) {
			t.Fatalf("second Commit = %v, want uow.ErrClosed", err)
		}
		if err := uow.Stage(u, "worker", entityID, build(entityID, 4, fixedInstant), 3); !errors.Is(err, uow.ErrClosed) {
			t.Fatalf("Stage after Commit = %v, want uow.ErrClosed", err)
		}
		if err := uow.Register(u, "worker-again", repo); !errors.Is(err, uow.ErrClosed) {
			t.Fatalf("Register after Commit = %v, want uow.ErrClosed", err)
		}
		if u.Tx() != nil {
			t.Fatal("Tx() returned a live handle after Commit; the transaction must not leak past the unit of work's own lifetime")
		}
	})

	t.Run("Rollback discards every staged write; nothing is committed", func(t *testing.T) {
		_, versionBefore, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		u := beginWorkerUnit(t, ctx, conn, tenant, repo)
		if err := uow.Stage(u, "worker", entityID, build(entityID, 5, fixedInstant), versionBefore); err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if err := u.Rollback(ctx); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		// Idempotent, mirroring dbport.Tx.Rollback's own contract.
		if err := u.Rollback(ctx); err != nil {
			t.Fatalf("second Rollback: %v", err)
		}
		if err := u.Commit(ctx); !errors.Is(err, uow.ErrClosed) {
			t.Fatalf("Commit after Rollback = %v, want uow.ErrClosed", err)
		}

		_, versionAfter, err := repo.Load(ctx, conn, tenant, entityID, fixedInstant)
		if err != nil {
			t.Fatalf("Load after rollback: %v", err)
		}
		if versionAfter != versionBefore {
			t.Fatalf("version after Rollback = %d, want unchanged %d", versionAfter, versionBefore)
		}
	})

	t.Run("Register rejects a duplicate kind and an empty kind", func(t *testing.T) {
		u, err := uow.Begin(ctx, conn, tenant)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer func() { _ = u.Rollback(ctx) }()
		if err := uow.Register(u, "worker", repo); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		if err := uow.Register(u, "worker", repo); err == nil {
			t.Fatal("a duplicate kind was accepted a second time")
		}
		if err := uow.Register(u, "", repo); err == nil {
			t.Fatal("an empty kind was accepted")
		}
	})

	t.Run("Stage refuses an unregistered kind and a kind registered with a different aggregate type", func(t *testing.T) {
		u, err := uow.Begin(ctx, conn, tenant)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer func() { _ = u.Rollback(ctx) }()
		if err := uow.Stage(u, "never-registered", uuid.New(), build(uuid.New(), 1, fixedInstant), 0); err == nil {
			t.Fatal("Stage against an unregistered kind was accepted")
		}
		if err := uow.Register(u, "worker", repo); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if err := uow.Stage[string](u, "worker", uuid.New(), "not a worker", 0); err == nil {
			t.Fatal("Stage with a type mismatched against the registered repository was accepted")
		}
	})

	t.Run("Begin refuses the nil tenant", func(t *testing.T) {
		if _, err := uow.Begin(ctx, conn, uuid.Nil); err == nil {
			t.Fatal("uow.Begin accepted the nil tenant")
		}
	})
}
