package recover_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	wfrecover "github.com/monstercameron/hcm-next/internal/workflow/recover"
)

// TestTodo_WF_RUN_003_Mutation removes, one at a time, the two guards that
// make a recovery safe, and shows what each of them was holding up.
//
// They are not redundant, and the two sub-cases below are why. The
// idempotency key is what makes the effect itself exactly-once: fold the
// attempt number into it and the recoverer duplicates the very effect it was
// recovering. The fence check is what refuses a superseded holder *before*
// it reaches the guard at all: remove it and the dead worker's dispatch runs
// all the way to the idempotency table, where the key is then the only thing
// left standing -- and remove both and the effect happens twice.
//
// Each case ends by showing that the unmutated path still behaves, so a
// failure above is a failure of the guard named and not of the recovery as a
// whole.
func TestTodo_WF_RUN_003_Mutation(t *testing.T) {
	ctx := context.Background()

	t.Run("an attempt-scoped idempotency key duplicates the business effect", func(t *testing.T) {
		db := pgtest.New(t)
		dead := newDeadWorker(t, db, "mutation-key")
		effect := newLedgerEffect()

		// The first worker commits its effect and dies before advancing.
		crashing := dead.recoverer(t, recovererConfig{
			effect: effect,
			fail:   &wfrecover.CrashAt{Phase: wfrecover.PhaseAfterDispatchBeforeResultCommit},
		})
		if _, err := crashing.Recover(ctx, dead.request()); err == nil {
			t.Fatal("the failpoint did not crash the first worker")
		}
		if effect.Dispatches() != 1 || ledgerRows(t, db, dead.tenant) != 1 {
			t.Fatalf("setup left dispatches=%d rows=%d, want 1 and 1",
				effect.Dispatches(), ledgerRows(t, db, dead.tenant))
		}

		// THE MUTATION: the recovery presents a key that varies with the
		// attempt, so the record the first worker committed is invisible to
		// it and it performs the effect all over again.
		mutated := dead.request()
		mutated.IdempotencyKey = wfrecover.AttemptKey(dead.instanceID, dead.nodeID) + ":attempt-3"
		r := dead.recoverer(t, recovererConfig{effect: effect, now: deadAt.Add(10 * time.Minute)})
		out, err := r.Recover(ctx, mutated)
		if err != nil {
			t.Fatalf("the mutated recovery failed for an unrelated reason: %v", err)
		}
		if out.Effect.Replayed {
			t.Fatal("the attempt-scoped key still found the stored record; the mutation did not take")
		}
		if got := effect.Dispatches(); got != 2 {
			t.Fatalf("effect dispatched %d times under an attempt-scoped key, want 2 -- "+
				"the duplicate this guard exists to prevent", got)
		}
		if got := ledgerRows(t, db, dead.tenant); got != 2 {
			t.Fatalf("%d ledger rows under an attempt-scoped key, want 2", got)
		}
	})

	t.Run("the derived key keeps the same recovery to exactly one effect", func(t *testing.T) {
		db := pgtest.New(t)
		dead := newDeadWorker(t, db, "mutation-key-control")
		effect := newLedgerEffect()

		crashing := dead.recoverer(t, recovererConfig{
			effect: effect,
			fail:   &wfrecover.CrashAt{Phase: wfrecover.PhaseAfterDispatchBeforeResultCommit},
		})
		if _, err := crashing.Recover(ctx, dead.request()); err == nil {
			t.Fatal("the failpoint did not crash the first worker")
		}
		r := dead.recoverer(t, recovererConfig{effect: effect, now: deadAt.Add(10 * time.Minute)})
		out, err := r.Recover(ctx, dead.request())
		if err != nil {
			t.Fatalf("the unmutated recovery failed: %v", err)
		}
		if !out.Effect.Replayed {
			t.Fatal("the unmutated recovery re-ran a committed effect")
		}
		if got := effect.Dispatches(); got != 1 {
			t.Fatalf("effect dispatched %d times under the derived key, want exactly 1", got)
		}
		if got := ledgerRows(t, db, dead.tenant); got != 1 {
			t.Fatalf("%d ledger rows under the derived key, want exactly 1", got)
		}
	})

	t.Run("removing the fence check admits the superseded holder's dispatch", func(t *testing.T) {
		db := pgtest.New(t)
		dead := newDeadWorker(t, db, "mutation-fence")
		effect := newLedgerEffect()

		recovering := dead.recoverer(t, recovererConfig{effect: effect})
		if _, err := recovering.Recover(ctx, dead.request()); err != nil {
			t.Fatalf("setup recovery: %v", err)
		}
		zombieConn := appConn(t, db)
		staleFence := dead.fence.RuntimeFence(deadAt.Add(time.Minute))
		lateAt := deadAt.Add(time.Minute)

		// Unmutated: the fence refuses the dead worker before the guard, and
		// therefore before the idempotency table is even read.
		fenced := dead.recoverer(t, recovererConfig{conn: zombieConn, effect: effect, holder: workerA})
		var fencedStatements *recordingTx
		err := inTenantTxErr(zombieConn, dead.tenant, func(tx dbport.Tx) error {
			fencedStatements = &recordingTx{tx: tx}
			_, derr := fenced.DispatchEffect(ctx, fencedStatements, dead.request(), 1, staleFence, lateAt)
			return derr
		})
		if err == nil {
			t.Fatal("the fenced path admitted the superseded holder")
		}
		if got := wfrecover.CodeOf(err); got != wfrecover.CodeFenceRefused {
			t.Fatalf("code = %q, want %q (%v)", got, wfrecover.CodeFenceRefused, err)
		}
		if fencedStatements.touched("idempotency_record") {
			t.Fatalf("the fenced refusal still read the idempotency table: %v", fencedStatements.statements())
		}

		// THE MUTATION: a verifier that never refuses. The same dispatch now
		// reaches the guard, which is the point -- the fence was what kept it
		// away from there.
		unfenced := dead.recoverer(t, recovererConfig{
			conn: zombieConn, effect: effect, holder: workerA, verifier: acceptAnyFence{},
		})
		var unfencedStatements *recordingTx
		before := effect.Dispatches()
		err = inTenantTxErr(zombieConn, dead.tenant, func(tx dbport.Tx) error {
			unfencedStatements = &recordingTx{tx: tx}
			_, derr := unfenced.DispatchEffect(ctx, unfencedStatements, dead.request(), 1, staleFence, lateAt)
			return derr
		})
		if err != nil {
			t.Fatalf("the unfenced dispatch failed for an unrelated reason: %v", err)
		}
		if !unfencedStatements.touched("idempotency_record") {
			t.Fatalf("the unfenced dispatch never reached the guard: %v", unfencedStatements.statements())
		}
		if effect.Dispatches() != before {
			t.Fatalf("the idempotency key did not hold once the fence was removed: %d -> %d",
				before, effect.Dispatches())
		}
	})

	t.Run("removing both guards runs the effect twice", func(t *testing.T) {
		db := pgtest.New(t)
		dead := newDeadWorker(t, db, "mutation-both")
		effect := newLedgerEffect()

		recovering := dead.recoverer(t, recovererConfig{effect: effect})
		if _, err := recovering.Recover(ctx, dead.request()); err != nil {
			t.Fatalf("setup recovery: %v", err)
		}
		if effect.Dispatches() != 1 || ledgerRows(t, db, dead.tenant) != 1 {
			t.Fatalf("setup left dispatches=%d rows=%d, want 1 and 1",
				effect.Dispatches(), ledgerRows(t, db, dead.tenant))
		}

		zombieConn := appConn(t, db)
		unguarded := dead.recoverer(t, recovererConfig{
			conn: zombieConn, effect: effect, holder: workerA, verifier: acceptAnyFence{},
		})
		mutated := dead.request()
		mutated.IdempotencyKey = wfrecover.AttemptKey(dead.instanceID, dead.nodeID) + ":attempt-1"
		err := inTenantTxErr(zombieConn, dead.tenant, func(tx dbport.Tx) error {
			_, derr := unguarded.DispatchEffect(ctx, tx, mutated, 1,
				dead.fence.RuntimeFence(deadAt.Add(time.Minute)), deadAt.Add(time.Minute))
			return derr
		})
		if err != nil {
			t.Fatalf("the unguarded dispatch failed for an unrelated reason: %v", err)
		}
		if got := effect.Dispatches(); got != 2 {
			t.Fatalf("effect dispatched %d times with both guards removed, want 2", got)
		}
		if got := ledgerRows(t, db, dead.tenant); got != 2 {
			t.Fatalf("%d ledger rows with both guards removed, want 2", got)
		}
	})

	t.Run("a key already bound to a different request is refused by digest, not dispatched", func(t *testing.T) {
		db := pgtest.New(t)
		dead := newDeadWorker(t, db, "mutation-digest")
		effect := newLedgerEffect()

		// The dead worker got as far as committing an effect under this
		// node's key, but under a request that no longer digests the same --
		// the shape of a worker that resumed from a stale view of what it was
		// supposed to be doing. TX-006's own conflict is what refuses the
		// recovery, and nothing is dispatched and nothing is overwritten.
		seedConflictingRecord(t, dead, "a different request bound this key first")

		r := dead.recoverer(t, recovererConfig{effect: effect})
		_, err := r.Recover(ctx, dead.request())
		if err == nil {
			t.Fatal("a key bound to a different request was reused as if it were this one")
		}
		if got := wfrecover.CodeOf(err); got != wfrecover.CodeEffectFailed {
			t.Fatalf("code = %q, want %q (%v)", got, wfrecover.CodeEffectFailed, err)
		}
		if code := idempotency.CodeOf(err); code != idempotency.CodeConflict {
			t.Fatalf("the wrapped TX-006 code = %q, want %q (%v)", code, idempotency.CodeConflict, err)
		}
		if got := effect.Dispatches(); got != 0 {
			t.Fatalf("a conflicting key still dispatched %d effects", got)
		}
		if got := ledgerRows(t, db, dead.tenant); got != 0 {
			t.Fatalf("a conflicting key still wrote %d ledger rows", got)
		}
		attempts := nodeAttempts(t, dead.conn, dead)
		if last := attempts[len(attempts)-1]; last.Status.Finished() {
			t.Fatalf("a refused recovery still finished the node: %s", last.Status)
		}
	})
}

// seedConflictingRecord completes the recovery's own idempotency scope under
// a digest that is deliberately not the request's, so the next dispatch on
// that key meets TX-006's IDEMPOTENCY_CONFLICT.
func seedConflictingRecord(t *testing.T, dead deadWorker, marker string) {
	t.Helper()
	other := sha256Hex(marker)
	if other == dead.request().EffectDigest() {
		t.Fatal("the seeded digest collided with the request's own")
	}
	inTenantTx(t, dead.conn, dead.tenant, func(tx dbport.Tx) error {
		store := idempotency.PostgresStore{}
		scope := dead.request().Scope()
		if _, _, err := store.Reserve(context.Background(), tx, scope, other, retention, bootAt); err != nil {
			return err
		}
		_, err := store.Complete(context.Background(), tx, scope,
			idempotency.ResultIdentity{ResultRef: "someone-elses-result"}, bootAt)
		return err
	})
}
