package recover_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_RUN_003_Fault walks every declared persistence boundary. For
// each one it kills the recovering worker there, states exactly what the
// durable rows hold at that boundary, and then recovers again from those rows
// alone -- asserting the two properties WF-RUN-003's RED clause names: no
// lost work (the node finishes) and no duplicated business effect (exactly
// one effect dispatch and exactly one ledger row, every time).
func TestTodo_WF_RUN_003_Fault(t *testing.T) {
	ctx := context.Background()

	for _, phase := range wfrecover.Phases() {
		phase := phase
		t.Run(string(phase), func(t *testing.T) {
			db := pgtest.New(t)
			dead := newDeadWorker(t, db, "fault-"+strings.ToLower(string(phase)))
			effect := newLedgerEffect()

			fp := &wfrecover.CrashAt{Phase: phase}
			crashing := dead.recoverer(t, recovererConfig{effect: effect, fail: fp})
			crashed, err := crashing.Recover(ctx, dead.request())

			if err == nil {
				t.Fatalf("the failpoint at %s did not crash the recovery", phase)
			}
			if !errors.Is(err, wfrecover.ErrCrashed) {
				t.Fatalf("the failpoint refusal does not classify as ErrCrashed: %v", err)
			}
			if got := wfrecover.CodeOf(err); got != wfrecover.CodeCrashInjected {
				t.Fatalf("code = %q, want %q (%v)", got, wfrecover.CodeCrashInjected, err)
			}
			if got := wfrecover.PhaseOf(err); got != phase {
				t.Fatalf("the refusal names boundary %q, want %q", got, phase)
			}
			if crashed.CrashedAt != phase {
				t.Fatalf("receipt names boundary %q, want %q", crashed.CrashedAt, phase)
			}
			if !fp.Fired() {
				t.Fatal("the failpoint reported it had not fired")
			}

			// What the boundary means, stated as durable rows rather than as
			// an expectation about the code that produced them.
			attempts := nodeAttempts(t, dead.conn, dead)
			_, hasRecord := idempotencyStatus(t, dead.conn, dead)
			switch phase {
			case wfrecover.PhaseBeforeNodeStateCommit:
				if len(attempts) != 1 {
					t.Fatalf("%d attempts after a crash before the node-state commit, want the original 1", len(attempts))
				}
				if attempts[0].Status != runtime.NodeRunning {
					t.Fatalf("the dead attempt is %s, want the untouched RUNNING", attempts[0].Status)
				}
				if hasRecord || effect.Dispatches() != 0 || ledgerRows(t, db, dead.tenant) != 0 {
					t.Fatal("a crash before the node-state commit still left an effect behind")
				}
			case wfrecover.PhaseAfterNodeStateCommitBeforeDispatch:
				if len(attempts) != 2 {
					t.Fatalf("%d attempts after the node-state commit, want 2", len(attempts))
				}
				if attempts[1].Status != runtime.NodeReady {
					t.Fatalf("the scheduled attempt is %s, want READY", attempts[1].Status)
				}
				if hasRecord || effect.Dispatches() != 0 || ledgerRows(t, db, dead.tenant) != 0 {
					t.Fatal("a crash before the dispatch still dispatched an effect")
				}
			case wfrecover.PhaseAfterDispatchBeforeResultCommit:
				if effect.Dispatches() != 1 {
					t.Fatalf("effect dispatched %d times, want 1", effect.Dispatches())
				}
				if got := ledgerRows(t, db, dead.tenant); got != 1 {
					t.Fatalf("%d ledger rows, want 1", got)
				}
				rec, ok := idempotencyStatus(t, dead.conn, dead)
				if !ok || rec.Status != idempotency.StatusCompleted {
					t.Fatalf("the committed effect left no COMPLETED record (found=%v, %+v)", ok, rec)
				}
				if attempts[len(attempts)-1].Status.Finished() {
					t.Fatal("the advancement committed even though the crash was before it")
				}
			case wfrecover.PhaseAfterResultCommit:
				if attempts[len(attempts)-1].Status != runtime.NodeSucceeded {
					t.Fatalf("the recovered attempt is %s, want SUCCEEDED", attempts[len(attempts)-1].Status)
				}
				if effect.Dispatches() != 1 || ledgerRows(t, db, dead.tenant) != 1 {
					t.Fatalf("effect dispatches=%d ledger rows=%d, want 1 and 1",
						effect.Dispatches(), ledgerRows(t, db, dead.tenant))
				}
			}

			// A later worker picks the node up from the durable rows alone and
			// finishes it. The failpoint is deliberately reused: CrashAt fires
			// once, so the retry proceeds past the boundary that killed the
			// first attempt.
			again := dead.recoverer(t, recovererConfig{
				effect: effect, fail: fp, now: deadAt.Add(10 * time.Minute),
			})
			out, err := again.Recover(ctx, dead.request())
			if err != nil {
				t.Fatalf("recovering after a crash at %s: %v", phase, err)
			}

			if phase == wfrecover.PhaseAfterResultCommit {
				if out.Recovered {
					t.Fatal("a node whose result had already committed was recovered again")
				}
				if out.Assessment.Disposition != wfrecover.DispositionNothingToRecover {
					t.Fatalf("disposition = %q, want %q",
						out.Assessment.Disposition, wfrecover.DispositionNothingToRecover)
				}
			} else {
				if !out.Recovered {
					t.Fatalf("the node was not recovered after a crash at %s", phase)
				}
				wantReplay := phase == wfrecover.PhaseAfterDispatchBeforeResultCommit
				if out.Effect.Replayed != wantReplay {
					t.Fatalf("replayed = %v, want %v: a committed effect is replayed and a missing one is performed",
						out.Effect.Replayed, wantReplay)
				}
				if out.Advance.CompletedState != string(runtime.NodeSucceeded) {
					t.Fatalf("the recovered attempt completed as %q, want SUCCEEDED", out.Advance.CompletedState)
				}
			}

			// No lost work.
			final := nodeAttempts(t, dead.conn, dead)
			if last := final[len(final)-1]; last.Status != runtime.NodeSucceeded {
				t.Fatalf("after recovering from %s the node is %s, want SUCCEEDED", phase, last.Status)
			}
			inst := loadInstance(t, dead.conn, dead)
			if inst.RuntimeStatus != runtime.InstanceRunning {
				t.Fatalf("after recovering from %s the instance is %s, want RUNNING", phase, inst.RuntimeStatus)
			}

			// No duplicated business effect.
			if got := effect.Dispatches(); got != 1 {
				t.Fatalf("after recovering from %s the effect was dispatched %d times, want exactly 1", phase, got)
			}
			if got := ledgerRows(t, db, dead.tenant); got != 1 {
				t.Fatalf("after recovering from %s there are %d ledger rows, want exactly 1", phase, got)
			}
		})
	}
}
