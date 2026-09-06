package recover_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	wfrecover "github.com/monstercameron/hcm-next/internal/workflow/recover"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// TestTodo_WF_RUN_003 is the PRIMARY test: a node whose worker died with an
// expired lease is recovered by a second worker, which takes the lease over
// under a new fence, schedules the next attempt, performs the missing effect
// exactly once under the dead attempt's own idempotency key, and advances the
// instance -- while the dead worker's late write, presented under the fence
// it still carries, is refused with zero mutation.
func TestTodo_WF_RUN_003(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	dead := newDeadWorker(t, db, "wfrun003")
	effect := newLedgerEffect()
	r := dead.recoverer(t, recovererConfig{effect: effect})

	receipt, err := r.Recover(ctx, dead.request())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}

	t.Run("the assessment reads the dead attempt off the durable rows", func(t *testing.T) {
		a := receipt.Assessment
		if a.Disposition != wfrecover.DispositionExecuteEffect {
			t.Fatalf("disposition = %q, want %q -- nothing was recorded, so the effect is missing",
				a.Disposition, wfrecover.DispositionExecuteEffect)
		}
		if !a.LeaseHeld || !a.LeaseExpired {
			t.Fatalf("lease held=%v expired=%v; the fixture left a lapsed lease", a.LeaseHeld, a.LeaseExpired)
		}
		if a.HolderID != workerA.HolderID() {
			t.Fatalf("holder = %q, want the worker that died (%q)", a.HolderID, workerA.HolderID())
		}
		if a.DeadAttempt != 1 || a.DeadAttemptStatus != runtime.NodeRunning {
			t.Fatalf("dead attempt = %d/%s, want 1/RUNNING", a.DeadAttempt, a.DeadAttemptStatus)
		}
		if a.NextAttempt != 2 {
			t.Fatalf("next attempt = %d, want 2", a.NextAttempt)
		}
	})

	t.Run("a new attempt is scheduled under a strictly newer fence", func(t *testing.T) {
		if !receipt.Recovered || receipt.Attempt != 2 {
			t.Fatalf("recovered=%v attempt=%d, want true/2", receipt.Recovered, receipt.Attempt)
		}
		if receipt.Fence.Token <= dead.fence.Token {
			t.Fatalf("recovering fence token %d does not exceed the dead worker's %d",
				receipt.Fence.Token, dead.fence.Token)
		}
		if receipt.Fence.HolderID != workerB.HolderID() {
			t.Fatalf("fence holder = %q, want %q", receipt.Fence.HolderID, workerB.HolderID())
		}
		if receipt.Lease.Kind != "TAKEN_OVER" {
			t.Fatalf("lease evidence = %q, want TAKEN_OVER -- a lapsed holder was retired to make room",
				receipt.Lease.Kind)
		}
	})

	t.Run("the missing effect ran exactly once and its result is durable", func(t *testing.T) {
		if receipt.Effect.Replayed {
			t.Fatal("nothing had been recorded, yet the recovery reported a replay")
		}
		if got := effect.Dispatches(); got != 1 {
			t.Fatalf("effect dispatched %d times, want exactly 1", got)
		}
		if got := ledgerRows(t, db, dead.tenant); got != 1 {
			t.Fatalf("%d ledger rows, want exactly 1", got)
		}
		rec, found := idempotencyStatus(t, dead.conn, dead)
		if !found {
			t.Fatal("no idempotency record was stored for the recovered effect")
		}
		if rec.Status != idempotency.StatusCompleted {
			t.Fatalf("idempotency record status = %q, want COMPLETED", rec.Status)
		}
		if rec.RequestDigest != dead.request().EffectDigest() {
			t.Fatalf("stored digest %q is not the request's own %q", rec.RequestDigest, dead.request().EffectDigest())
		}
	})

	t.Run("the recovered attempt advanced the instance", func(t *testing.T) {
		if receipt.Advance.NodeID != dead.nodeID || receipt.Advance.Attempt != 2 {
			t.Fatalf("advance receipt = %s/%d, want %s/2", receipt.Advance.NodeID, receipt.Advance.Attempt, dead.nodeID)
		}
		if receipt.Advance.CompletedState != string(runtime.NodeSucceeded) {
			t.Fatalf("completed state = %q, want SUCCEEDED", receipt.Advance.CompletedState)
		}
		if len(receipt.Advance.Continuations) == 0 {
			t.Fatal("the recovered advancement derived no continuation at all")
		}
		attempts := nodeAttempts(t, dead.conn, dead)
		if len(attempts) != 2 {
			t.Fatalf("%d recorded attempts, want 2 (the retired one and the recovered one)", len(attempts))
		}
		if attempts[0].Status != runtime.NodeRetrying {
			t.Fatalf("the dead attempt is %s, want RETRYING", attempts[0].Status)
		}
		if attempts[0].ErrorClass != wfrecover.ErrorClassWorkerDied {
			t.Fatalf("the dead attempt's error class = %q, want %q",
				attempts[0].ErrorClass, wfrecover.ErrorClassWorkerDied)
		}
		if attempts[1].Status != runtime.NodeSucceeded {
			t.Fatalf("the recovered attempt is %s, want SUCCEEDED", attempts[1].Status)
		}
		inst := loadInstance(t, dead.conn, dead)
		if inst.RuntimeStatus != runtime.InstanceRunning {
			t.Fatalf("instance status = %s, want RUNNING", inst.RuntimeStatus)
		}
		if inst.InstanceVersion != receipt.Advance.NewInstanceVersion {
			t.Fatalf("stored instance version %d disagrees with the receipt's %d",
				inst.InstanceVersion, receipt.Advance.NewInstanceVersion)
		}
	})

	t.Run("the dead worker's late dispatch is refused under its stale fence", func(t *testing.T) {
		before := effect.Dispatches()
		err := inTenantTxErr(dead.conn, dead.tenant, func(tx dbport.Tx) error {
			_, derr := r.DispatchEffect(ctx, tx, dead.request(), 1,
				dead.fence.RuntimeFence(deadAt.Add(time.Second)), deadAt.Add(time.Second))
			return derr
		})
		if err == nil {
			t.Fatal("the dead worker dispatched an effect after being superseded")
		}
		if got := wfrecover.CodeOf(err); got != wfrecover.CodeFenceRefused {
			t.Fatalf("code = %q, want %q (%v)", got, wfrecover.CodeFenceRefused, err)
		}
		if !errors.Is(err, wfrecover.ErrFenceRefused) {
			t.Fatalf("refusal does not classify as ErrFenceRefused: %v", err)
		}
		if effect.Dispatches() != before {
			t.Fatalf("a refused dispatch still ran the effect: %d -> %d", before, effect.Dispatches())
		}
		if got := ledgerRows(t, db, dead.tenant); got != 1 {
			t.Fatalf("%d ledger rows after the refused late write, want still 1", got)
		}
	})

	t.Run("a second recovery of a finished node has nothing to do", func(t *testing.T) {
		again := dead.recoverer(t, recovererConfig{effect: effect, now: deadAt.Add(10 * time.Minute)})
		out, err := again.Recover(ctx, dead.request())
		if err != nil {
			t.Fatalf("re-recovering a finished node: %v", err)
		}
		if out.Recovered {
			t.Fatal("a finished node was recovered a second time")
		}
		if out.Assessment.Disposition != wfrecover.DispositionNothingToRecover {
			t.Fatalf("disposition = %q, want %q", out.Assessment.Disposition, wfrecover.DispositionNothingToRecover)
		}
		if got := effect.Dispatches(); got != 1 {
			t.Fatalf("effect dispatched %d times overall, want exactly 1", got)
		}
	})
}

// TestTodo_WF_RUN_003_LeaseLive pins the refusal that keeps recovery honest:
// a node whose holder is still inside its lease window is not recoverable,
// because recovering it would be this package declaring a live worker dead.
func TestTodo_WF_RUN_003_LeaseLive(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	dead := newDeadWorker(t, db, "wfrun003-live")
	effect := newLedgerEffect()

	// One second after the lease was taken, worker A is plainly alive.
	r := dead.recoverer(t, recovererConfig{effect: effect, now: bootAt.Add(time.Second)})
	out, err := r.Recover(ctx, dead.request())
	if err == nil {
		t.Fatal("a live worker's node was recovered")
	}
	if got := wfrecover.CodeOf(err); got != wfrecover.CodeLeaseLive {
		t.Fatalf("code = %q, want %q (%v)", got, wfrecover.CodeLeaseLive, err)
	}
	if !errors.Is(err, wfrecover.ErrLeaseLive) {
		t.Fatalf("refusal does not classify as ErrLeaseLive: %v", err)
	}
	if out.Recovered {
		t.Fatal("the receipt claims a live worker's node was recovered")
	}
	if got := effect.Dispatches(); got != 0 {
		t.Fatalf("a refused recovery dispatched %d effects", got)
	}
	if got := ledgerRows(t, db, dead.tenant); got != 0 {
		t.Fatalf("a refused recovery wrote %d ledger rows", got)
	}
	if attempts := nodeAttempts(t, dead.conn, dead); len(attempts) != 1 {
		t.Fatalf("a refused recovery left %d attempts, want the original 1", len(attempts))
	}
}
