package uow

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// Reporter is the minimal test-failure surface [RunConformance] needs.
// *testing.T and *testing.B both satisfy it; declaring it locally keeps this
// file free of a "testing" import, so it stays an ordinary (non-_test.go)
// file any package's own tests can call without pulling testing into a
// production build.
type Reporter interface {
	Helper()
	Fatalf(format string, args ...any)
}

// ConformanceCase supplies everything [RunConformance] needs to drive an
// [AggregateRepository][T] without knowing T's shape.
type ConformanceCase[T any] struct {
	// Repo is the implementation under test.
	Repo AggregateRepository[T]
	// Exec is the [dbport.Conn] Repo's calls run against. A repository that
	// ignores its ex parameter (such as [MemoryRepository]) accepts nil here.
	Exec dbport.Conn
	// Tenant scopes every call RunConformance makes.
	Tenant uuid.UUID
	// Build returns the seq'th revision (1-based) RunConformance should
	// append for entityID. Every revision Build returns for one entityID
	// should be current at businessAt (open-ended EffectiveTo, EffectiveFrom
	// at or before businessAt for a bitemporal implementation), so a
	// bitemporal repository's Load(..., businessAt) finds whichever revision
	// is the most recently appended one, the same as
	// [MemoryRepository.Load]'s always-most-recent behavior.
	Build func(entityID uuid.UUID, seq int, businessAt time.Time) T
}

// RunConformance drives the shared load/append/list contract every
// [AggregateRepository][T] implementation -- this package's own two, and any
// later one over a different DB-008/009/010 table -- must satisfy:
//
//   - Load on an entity with no revisions fails.
//   - Save at expectedVersion 0 creates the first revision; Save at
//     expectedVersion 0 again is refused with [*ErrStaleVersion] naming the
//     true (now 1) version, and appends nothing.
//   - Save at the correct next expectedVersion succeeds and advances the
//     version by exactly one; Save at a version that has since gone stale is
//     refused with [*ErrStaleVersion] naming the true current version, and
//     appends nothing.
//   - After three successful appends, Load reports version 3 and
//     ListRevisions returns exactly those three revisions, oldest first.
func RunConformance[T any](t Reporter, c ConformanceCase[T]) {
	t.Helper()
	ctx := context.Background()
	entityID := uuid.New()
	businessAt := time.Now().UTC()

	if _, _, err := c.Repo.Load(ctx, c.Exec, c.Tenant, entityID, businessAt); err == nil {
		t.Fatalf("Load on an entity with no revisions: want an error, got none")
	}

	if err := c.Repo.Save(ctx, c.Exec, c.Tenant, entityID, c.Build(entityID, 1, businessAt), 0); err != nil {
		t.Fatalf("Save the first revision at expectedVersion 0: %v", err)
	}
	assertStale(t, "a second first revision at expectedVersion 0", 1,
		c.Repo.Save(ctx, c.Exec, c.Tenant, entityID, c.Build(entityID, 1, businessAt), 0))

	if err := c.Repo.Save(ctx, c.Exec, c.Tenant, entityID, c.Build(entityID, 2, businessAt), 1); err != nil {
		t.Fatalf("Save the second revision at expectedVersion 1: %v", err)
	}
	assertStale(t, "a third revision at the now-stale expectedVersion 1", 2,
		c.Repo.Save(ctx, c.Exec, c.Tenant, entityID, c.Build(entityID, 3, businessAt), 1))

	if err := c.Repo.Save(ctx, c.Exec, c.Tenant, entityID, c.Build(entityID, 3, businessAt), 2); err != nil {
		t.Fatalf("Save the third revision at expectedVersion 2: %v", err)
	}

	_, version, err := c.Repo.Load(ctx, c.Exec, c.Tenant, entityID, businessAt)
	if err != nil {
		t.Fatalf("Load after three revisions: %v", err)
	}
	if version != 3 {
		t.Fatalf("Load after three revisions: version = %d, want 3", version)
	}

	revs, err := c.Repo.ListRevisions(ctx, c.Exec, c.Tenant, entityID)
	if err != nil {
		t.Fatalf("ListRevisions after three revisions: %v", err)
	}
	if len(revs) != 3 {
		t.Fatalf("ListRevisions after three revisions: len = %d, want 3", len(revs))
	}
}

func assertStale(t Reporter, label string, wantActual Version, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("Save %s: want *ErrStaleVersion, got no error", label)
		return
	}
	var stale *ErrStaleVersion
	if !errors.As(err, &stale) {
		t.Fatalf("Save %s: want *ErrStaleVersion, got %v (%T)", label, err, err)
		return
	}
	if stale.Actual != wantActual {
		t.Fatalf("Save %s: ErrStaleVersion.Actual = %d, want %d", label, stale.Actual, wantActual)
	}
}
