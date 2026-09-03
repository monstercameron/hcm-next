package seed_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/seed"
)

// TestTodo_DB_019_Race drives many concurrent Seed calls at the same tenant,
// each on its own connection and transaction, and proves the plan still lands
// exactly once per registration: INSERT ... ON CONFLICT DO NOTHING is
// PostgreSQL's own concurrency-safe upsert primitive, so no goroutine should
// ever see a unique-violation error, and the final row count must equal the
// plan size exactly, not some multiple of it. This machine builds
// windows/arm64 without -race, so the proof is the observed row count and
// absence of unexpected errors, not the Go race detector.
func TestTodo_DB_019_Race(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "seed-race-tenant")

	plan, err := seed.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	const concurrency = 8
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			ctx := context.Background()
			conn := db.NewConn(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				errs <- fmt.Errorf("worker %d: begin: %w", worker, err)
				return
			}
			if _, err := seed.Seed(ctx, tx, tenantID); err != nil {
				_ = tx.Rollback(ctx)
				errs <- fmt.Errorf("worker %d: seed: %w", worker, err)
				return
			}
			if err := tx.Commit(ctx); err != nil {
				errs <- fmt.Errorf("worker %d: commit: %w", worker, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if got := countDefinitionVersions(t, db, tenantID); got != len(plan) {
		t.Fatalf("definition_version holds %d rows after %d concurrent seeds, want exactly %d",
			got, concurrency, len(plan))
	}
}
