package idempotency_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
)

// TestTodo_TX_006_Race proves the RED clause "concurrent same-key calls
// commit twice" is impossible: two real, independent connections race
// Guard.Reserve on the exact same scope and digest at once, and the effect
// still runs exactly once. The loser observes the winner's own stored
// identity, never a second one of its own.
func TestTodo_TX_006_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "tx006-race")
	store := idempotency.PostgresStore{}
	scope := defaultScope(tenant, "race")
	digest := digestOf("race-fixture")

	const attempts = 8
	var calls int32
	results := make([]idempotency.Record, attempts)
	errs := make([]error, attempts)

	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	done.Add(attempts)

	for i := 0; i < attempts; i++ {
		i := i
		conn := appConn(t, db)
		go func() {
			defer done.Done()
			start.Wait()

			tx, err := conn.Begin(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			runErr := func() error {
				if e := tenancy.WithTenant(ctx, tx, tenant); e != nil {
					return e
				}
				rec, guardErr := idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant,
					func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
						atomic.AddInt32(&calls, 1)
						return idempotency.ResultIdentity{
							ResultRef: "race-result",
							EventRef:  "attempt-" + uuid.NewString(),
						}, nil
					})
				results[i] = rec
				return guardErr
			}()
			if runErr != nil {
				_ = tx.Rollback(ctx)
				errs[i] = runErr
				return
			}
			errs[i] = tx.Commit(ctx)
		}()
	}

	start.Done()
	done.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("effect ran %d times across %d concurrent attempts, want exactly 1", got, attempts)
	}

	var succeeded, conflicted, inProgress int
	var winningEventRef string
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
			if results[i].Identity.EventRef == "" {
				t.Fatalf("attempt %d succeeded with no stored identity", i)
			}
			if winningEventRef == "" {
				winningEventRef = results[i].Identity.EventRef
			} else if results[i].Identity.EventRef != winningEventRef {
				t.Fatalf("attempt %d succeeded with a different identity (%q) than the winner (%q); two effects committed",
					i, results[i].Identity.EventRef, winningEventRef)
			}
		case idempotency.CodeOf(err) == idempotency.CodeInProgress:
			inProgress++
		case idempotency.CodeOf(err) == idempotency.CodeConflict:
			conflicted++
		default:
			t.Fatalf("attempt %d failed unexpectedly: %v", i, err)
		}
	}

	if succeeded == 0 {
		t.Fatal("no concurrent attempt ever succeeded")
	}
	if conflicted != 0 {
		t.Fatalf("%d attempt(s) saw a digest conflict, but every attempt used the same digest", conflicted)
	}
	t.Logf("attempts=%d succeeded=%d in_progress=%d", attempts, succeeded, inProgress)
}
