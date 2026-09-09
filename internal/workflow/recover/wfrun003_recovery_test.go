package recover_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	wfrecover "github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_RUN_003_Recovery restarts the recovery from the durable rows
// alone.
//
// The first worker dies after its effect committed and before its
// advancement did -- the hardest boundary, because finishing correctly from
// there requires knowing that the effect already happened. Nothing at all is
// carried over from that worker: the second worker is built on a fresh
// connection, with a freshly compiled plan, and its request is assembled from
// identifiers read back out of the database rather than from the fixture's
// own variables. If any part of the disposition lived in the dead process's
// memory, this test could not pass.
func TestTodo_WF_RUN_003_Recovery(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	dead := newDeadWorker(t, db, "wfrun003-recovery")
	effect := newLedgerEffect()

	crashing := dead.recoverer(t, recovererConfig{
		effect: effect,
		fail:   &wfrecover.CrashAt{Phase: wfrecover.PhaseAfterDispatchBeforeResultCommit},
	})
	if _, err := crashing.Recover(ctx, dead.request()); err == nil {
		t.Fatal("the failpoint did not crash the first worker")
	}
	if effect.Dispatches() != 1 || ledgerRows(t, db, dead.tenant) != 1 {
		t.Fatalf("the first worker left dispatches=%d ledger rows=%d, want 1 and 1",
			effect.Dispatches(), ledgerRows(t, db, dead.tenant))
	}

	// --- everything below this line is a cold process ----------------------

	instanceID, nodeID := readOrphanedNode(t, db, dead.tenant)
	coldConn := appConn(t, db)
	coldPlan := referencePlan(t)
	coldReq := wfrecover.Request{
		TenantID: dead.tenant, InstanceID: instanceID, NodeID: nodeID,
		Plan: coldPlan, CorrelationID: "corr-cold-restart",
	}
	coldEffect := newLedgerEffect()
	cold := dead.recoverer(t, recovererConfig{
		conn: coldConn, effect: coldEffect, now: deadAt.Add(20 * time.Minute),
	})

	t.Run("the disposition is derived from the rows, and reading it writes nothing", func(t *testing.T) {
		var (
			assess wfrecover.Assessment
			rec    *recordingTx
		)
		inTenantTx(t, coldConn, dead.tenant, func(tx dbport.Tx) error {
			rec = &recordingTx{tx: tx}
			var err error
			assess, err = cold.Inspect(ctx, rec, coldReq, deadAt.Add(20*time.Minute))
			return err
		})
		if assess.Disposition != wfrecover.DispositionReplayResult {
			t.Fatalf("disposition = %q, want %q: the effect is already recorded",
				assess.Disposition, wfrecover.DispositionReplayResult)
		}
		if !assess.EffectRecorded {
			t.Fatal("the assessment did not see the committed effect")
		}
		if assess.EffectIdentity.EventRef == "" {
			t.Fatal("the assessment carries no stored result identity to replay from")
		}
		if assess.EffectDigestRecorded != coldReq.EffectDigest() {
			t.Fatalf("the stored digest %q is not the cold request's own %q -- the two workers "+
				"did not agree on the semantic identity of the effect",
				assess.EffectDigestRecorded, coldReq.EffectDigest())
		}
		if assess.DeadAttempt != 2 || assess.NextAttempt != 3 {
			t.Fatalf("dead attempt = %d, next = %d; want 2 and 3", assess.DeadAttempt, assess.NextAttempt)
		}
		if assess.DeadAttemptStatus != runtime.NodeReady {
			t.Fatalf("the orphaned attempt is %s, want the READY the dead worker scheduled", assess.DeadAttemptStatus)
		}
		if !assess.LeaseHeld || !assess.LeaseExpired {
			t.Fatalf("lease held=%v expired=%v; the dead worker's own lease has lapsed by now",
				assess.LeaseHeld, assess.LeaseExpired)
		}
		if rec.wrote() {
			t.Fatalf("Inspect issued a write: %v", rec.statements())
		}
	})

	t.Run("the cold process finishes the work by replaying the committed result", func(t *testing.T) {
		out, err := cold.Recover(ctx, coldReq)
		if err != nil {
			t.Fatalf("cold recovery: %v", err)
		}
		if !out.Recovered {
			t.Fatal("the cold process recovered nothing")
		}
		if !out.Effect.Replayed {
			t.Fatal("the cold process re-ran an effect that was already recorded")
		}
		if coldEffect.Dispatches() != 0 {
			t.Fatalf("the cold process dispatched %d effects, want 0 -- it should have replayed",
				coldEffect.Dispatches())
		}
		if coldEffect.Replays() != 1 {
			t.Fatalf("the cold process reconstructed the outcome %d times, want 1", coldEffect.Replays())
		}
		if out.Advance.CompletedState != string(runtime.NodeSucceeded) {
			t.Fatalf("the recovered attempt completed as %q, want SUCCEEDED", out.Advance.CompletedState)
		}
		if out.Advance.OutputDigest == "" {
			t.Fatal("the replayed outcome carried no output digest; it was not reconstructed from the ledger row")
		}
	})

	t.Run("the restart cost exactly nothing in duplicated effects", func(t *testing.T) {
		if got := ledgerRows(t, db, dead.tenant); got != 1 {
			t.Fatalf("%d ledger rows after the restart, want exactly 1", got)
		}
		if got := effect.Dispatches() + coldEffect.Dispatches(); got != 1 {
			t.Fatalf("%d effect dispatches across both processes, want exactly 1", got)
		}
		inst := loadInstance(t, coldConn, dead)
		if inst.RuntimeStatus != runtime.InstanceRunning {
			t.Fatalf("instance status = %s, want RUNNING", inst.RuntimeStatus)
		}
	})
}

// readOrphanedNode finds the instance and node of the one attempt that is
// still unfinished, using nothing but SQL. It is how the cold process learns
// what to recover: a real one reads a work queue, not a test variable.
func readOrphanedNode(t *testing.T, db *pgtest.DB, tenant uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	var (
		instanceID uuid.UUID
		nodeID     string
	)
	err := db.QueryRow(context.Background(), `
		SELECT instance_id, node_id FROM workflow_node_execution
		WHERE tenant_id = $1 AND status IN ('READY', 'RUNNING', 'WAITING')
		ORDER BY attempt DESC
		LIMIT 1`, tenant).Scan(&instanceID, &nodeID)
	if err != nil {
		t.Fatalf("read the orphaned node execution: %v", err)
	}
	return instanceID, nodeID
}
