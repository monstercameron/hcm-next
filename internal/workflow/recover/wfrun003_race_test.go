package recover_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	wfrecover "github.com/monstercameron/hcm-next/internal/workflow/recover"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// TestTodo_WF_RUN_003_Race runs the dead worker's late write and the
// recoverer's new attempt at the same time, on two genuinely separate
// connections, and asserts that exactly one business effect exists whichever
// of them gets there first.
//
// The two orderings are both legitimate and both safe, for different
// reasons. If the recoverer takes the lease first, the dead worker's fence is
// behind and its dispatch is refused before it reaches the idempotency table
// at all. If the dead worker slips its dispatch in first, its fence is still
// live and its effect commits -- and the recoverer then finds a COMPLETED
// record under the same key and replays it instead of running a second one.
// The assertion is therefore on the outcome, not on the ordering: one
// dispatch, one ledger row, node SUCCEEDED.
func TestTodo_WF_RUN_003_Race(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	dead := newDeadWorker(t, db, "wfrun003-race")
	effect := newLedgerEffect()

	// Two connections, two workers. Nothing is shared between them but the
	// database and the effect's own counters.
	zombieConn := appConn(t, db)
	recovering := dead.recoverer(t, recovererConfig{effect: effect})
	zombie := dead.recoverer(t, recovererConfig{
		conn: zombieConn, effect: effect, holder: workerA,
	})

	var (
		wg          sync.WaitGroup
		recoverErr  error
		zombieErr   error
		zombieRan   bool
		recoverDone wfrecover.Receipt
		start       = make(chan struct{})
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		recoverDone, recoverErr = recovering.Recover(ctx, dead.request())
	}()
	go func() {
		defer wg.Done()
		<-start
		// Worker A wakes up believing it still owns the node and dispatches
		// the effect it was in the middle of, under the fence it still
		// carries.
		zombieErr = inTenantTxErr(zombieConn, dead.tenant, func(tx dbport.Tx) error {
			out, err := zombie.DispatchEffect(ctx, tx, dead.request(), 1,
				dead.fence.RuntimeFence(deadAt), deadAt)
			if err != nil {
				return err
			}
			zombieRan = !out.Replayed
			return nil
		})
	}()
	close(start)
	wg.Wait()

	if recoverErr != nil {
		t.Fatalf("the recovery lost the race outright: %v", recoverErr)
	}
	if !recoverDone.Recovered {
		t.Fatal("the recovery reported it recovered nothing")
	}

	t.Run("exactly one business effect exists", func(t *testing.T) {
		if got := effect.Dispatches(); got != 1 {
			t.Fatalf("effect dispatched %d times under contention, want exactly 1", got)
		}
		if got := ledgerRows(t, db, dead.tenant); got != 1 {
			t.Fatalf("%d ledger rows under contention, want exactly 1", got)
		}
	})

	t.Run("the two workers did not both perform it", func(t *testing.T) {
		recovererRan := !recoverDone.Effect.Replayed
		if zombieErr == nil && zombieRan && recovererRan {
			t.Fatal("both the dead worker and the recoverer performed the effect")
		}
		if zombieErr != nil {
			// The ordinary outcome: the recoverer took the lease first and
			// the dead worker's fence was behind.
			if code := wfrecover.CodeOf(zombieErr); code != wfrecover.CodeFenceRefused &&
				code != wfrecover.CodeEffectFailed {
				t.Fatalf("the dead worker was refused with %q (%v); want a fence or guard refusal",
					code, zombieErr)
			}
		}
	})

	t.Run("the instance advanced exactly once", func(t *testing.T) {
		attempts := nodeAttempts(t, dead.conn, dead)
		last := attempts[len(attempts)-1]
		if last.Status != runtime.NodeSucceeded {
			t.Fatalf("the node ended at %s, want SUCCEEDED", last.Status)
		}
		succeeded := 0
		for _, a := range attempts {
			if a.Status == runtime.NodeSucceeded {
				succeeded++
			}
		}
		if succeeded != 1 {
			t.Fatalf("%d attempts of one node succeeded, want exactly 1", succeeded)
		}
	})

	t.Run("the dead worker cannot write anything afterwards either", func(t *testing.T) {
		before := effect.Dispatches()
		err := inTenantTxErr(zombieConn, dead.tenant, func(tx dbport.Tx) error {
			_, derr := zombie.DispatchEffect(ctx, tx, dead.request(), 1,
				dead.fence.RuntimeFence(deadAt.Add(time.Minute)), deadAt.Add(time.Minute))
			return derr
		})
		if err == nil {
			t.Fatal("the superseded worker dispatched an effect after the race settled")
		}
		if effect.Dispatches() != before {
			t.Fatalf("a refused late dispatch still ran the effect: %d -> %d", before, effect.Dispatches())
		}
		if got := ledgerRows(t, db, dead.tenant); got != 1 {
			t.Fatalf("%d ledger rows after the late write, want still 1", got)
		}
	})
}
