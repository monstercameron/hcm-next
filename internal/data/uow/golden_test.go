package uow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/uow"
)

// TestTodo_DB_018_Golden pins the exact wording and field shape of
// [uow.ConflictError] and [uow.ErrStaleVersion]: other packages will match
// against these fields (Kind, EntityID, ExpectedVersion, ActualVersion), and
// operators will read the message text in logs, so a change to either is a
// decision someone should make deliberately, not a side effect of an
// unrelated refactor. It runs against [uow.MemoryRepository] rather than
// pgtest because nothing here depends on PostgreSQL: the message format is
// this package's own contract, not the database's.
func TestTodo_DB_018_Golden(t *testing.T) {
	t.Parallel()

	tenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	entityID := uuid.MustParse("00000000-0000-0000-0000-0000000000e1")

	t.Run("ConflictError.Error text", func(t *testing.T) {
		err := &uow.ConflictError{
			Kind: "worker", EntityID: entityID,
			ExpectedVersion: 1, ActualVersion: 2,
		}
		const want = "uow: worker 00000000-0000-0000-0000-0000000000e1: optimistic version conflict: expected version 1, actual version 2"
		if got := err.Error(); got != want {
			t.Fatalf("ConflictError.Error() =\n%s\nwant\n%s", got, want)
		}
		if !errors.Is(err, uow.ErrUnitOfWork) {
			t.Fatal("ConflictError does not unwrap to uow.ErrUnitOfWork")
		}
	})

	t.Run("ErrStaleVersion.Error text", func(t *testing.T) {
		err := &uow.ErrStaleVersion{Actual: 7}
		const want = "uow: stale version: actual 7"
		if got := err.Error(); got != want {
			t.Fatalf("ErrStaleVersion.Error() =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("a deterministic three-revision sequence through a UnitOfWork produces exact versions and conflict fields", func(t *testing.T) {
		ctx := context.Background()
		repo := uow.NewMemoryRepository[string]()
		entity := entityID

		save := func(value string, expected uow.Version) error {
			return repo.Save(ctx, nil, tenant, entity, value, expected)
		}
		if err := save("rev-1", 0); err != nil {
			t.Fatalf("Save rev-1: %v", err)
		}
		if err := save("rev-2", 1); err != nil {
			t.Fatalf("Save rev-2: %v", err)
		}
		if err := save("rev-3", 2); err != nil {
			t.Fatalf("Save rev-3: %v", err)
		}

		_, version, err := repo.Load(ctx, nil, tenant, entity, fixedInstant)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if version != 3 {
			t.Fatalf("version = %d, want exactly 3", version)
		}

		conflictErr := save("rev-4-stale", 1)
		var stale *uow.ErrStaleVersion
		if !errors.As(conflictErr, &stale) {
			t.Fatalf("Save at a stale expectedVersion = %v (%T), want *uow.ErrStaleVersion", conflictErr, conflictErr)
		}
		if stale.Actual != 3 {
			t.Fatalf("ErrStaleVersion.Actual = %d, want 3", stale.Actual)
		}

		revs, err := repo.ListRevisions(ctx, nil, tenant, entity)
		if err != nil {
			t.Fatalf("ListRevisions: %v", err)
		}
		want := []string{"rev-1", "rev-2", "rev-3"}
		if len(revs) != len(want) {
			t.Fatalf("ListRevisions = %v, want %v", revs, want)
		}
		for i, w := range want {
			if revs[i] != w {
				t.Fatalf("ListRevisions[%d] = %q, want %q", i, revs[i], w)
			}
		}
	})
}
