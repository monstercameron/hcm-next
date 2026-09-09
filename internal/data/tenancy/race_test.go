package tenancy_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// TestTodo_DB_017_Race drives many tenants concurrently, each on its own
// connection and its own transaction, and proves none of them ever observes
// another's row: WithTenant's set_config(..., true) is transaction-local, so
// concurrent transactions on concurrent connections must never share or race
// on each other's session state. This machine builds windows/arm64 without
// -race, so the proof is behavioral (every goroutine independently checks
// the single row it is allowed to see) rather than relying on the race
// detector to flag a shared-state bug.
func TestTodo_DB_017_Race(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)

	const tenantCount = 12
	type fixture struct {
		id  uuid.UUID
		ref string
	}
	fixtures := make([]fixture, tenantCount)
	for i := range fixtures {
		id := insertTenant(t, db, fmt.Sprintf("race-tenant-%d", i))
		ref := fmt.Sprintf("authority:race-%d", i)
		insertAuthorityAssignment(t, db, id, ref)
		fixtures[i] = fixture{id: id, ref: ref}
	}

	var wg sync.WaitGroup
	errs := make(chan error, tenantCount)

	for _, f := range fixtures {
		wg.Add(1)
		go func(f fixture) {
			defer wg.Done()
			ctx := context.Background()

			conn := db.NewConn(t)
			if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
				errs <- fmt.Errorf("assume app role: %w", err)
				return
			}

			tx, err := conn.Begin(ctx)
			if err != nil {
				errs <- fmt.Errorf("begin: %w", err)
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			if err := tenancy.WithTenant(ctx, tx, f.id); err != nil {
				errs <- fmt.Errorf("scope to %s: %w", f.id, err)
				return
			}

			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM authority_assignment`).Scan(&n); err != nil {
				errs <- fmt.Errorf("count for %s: %w", f.id, err)
				return
			}
			if n != 1 {
				errs <- fmt.Errorf("tenant %s saw %d rows, want 1", f.id, n)
				return
			}

			var got string
			if err := tx.QueryRow(ctx, `SELECT authority_ref FROM authority_assignment`).Scan(&got); err != nil {
				errs <- fmt.Errorf("read ref for %s: %w", f.id, err)
				return
			}
			if got != f.ref {
				errs <- fmt.Errorf("tenant %s read %q, want %q (cross-tenant leak under concurrency)",
					f.id, got, f.ref)
			}
		}(f)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
