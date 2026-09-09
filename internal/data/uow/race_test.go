package uow_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/uow"
)

// TestTodo_DB_018_Race drives concurrent units of work, each on its own real
// connection, at the same expected version against the same worker aggregate:
// exactly one Commit succeeds, every other one is refused with a typed
// conflict, and the stored revision count advances by exactly one rather than
// once per contender.
func TestTodo_DB_018_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "db018-race")
	personRef := newPersonRef(t, db, tenant)
	build := workerBuilder(t, tenant, personRef)
	repo := uow.PostgresWorkerRepository{}
	setupConn := appConn(t, db, tenant)
	entityID := uuid.New()

	// Seed the first revision so every contender below races to append the
	// second one, all reading the same starting version 1.
	seed := beginWorkerUnit(t, ctx, setupConn, tenant, repo)
	if err := uow.Stage(seed, "worker", entityID, build(entityID, 1, fixedInstant), 0); err != nil {
		t.Fatalf("seed: Stage: %v", err)
	}
	if err := seed.Commit(ctx); err != nil {
		t.Fatalf("seed: Commit: %v", err)
	}

	const workers = 6
	conns := make([]*pgxadapter.Conn, workers)
	for i := range conns {
		conns[i] = appConn(t, db, tenant)
	}
	results := make([]error, workers)
	var startGate, done sync.WaitGroup
	startGate.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			startGate.Wait()
			u, err := uow.Begin(ctx, conns[i], tenant)
			if err != nil {
				results[i] = err
				return
			}
			if err := uow.Register(u, "worker", repo); err != nil {
				results[i] = err
				return
			}
			rev := build(entityID, 2, fixedInstant)
			if err := uow.Stage(u, "worker", entityID, rev, 1); err != nil {
				results[i] = err
				return
			}
			results[i] = u.Commit(ctx)
		}(i)
	}
	startGate.Done()
	done.Wait()

	wins, conflicts := 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			wins++
		default:
			if _, ok := uow.IsConflict(err); ok {
				conflicts++
			} else {
				t.Fatalf("worker %d: unexpected error: %v", i, err)
			}
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1 (conflicts=%d)", wins, conflicts)
	}
	if wins+conflicts != workers {
		t.Fatalf("wins(%d) + conflicts(%d) != workers(%d)", wins, conflicts, workers)
	}

	revs, err := repo.ListRevisions(ctx, setupConn, tenant, entityID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("ListRevisions after the race = %d rows, want exactly 2 (one seed + exactly one winner)", len(revs))
	}
}
