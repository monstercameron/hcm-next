package runtime_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// --- WF-RUN-031 ------------------------------------------------------------
//
// The PRIMARY test for this todo is
// [TestAdvanceRejectsAnOldReceiptAfterLaterProgress] in
// advancement_receipt_regression_test.go: a resubmitted advancement replays
// only while the instance still sits at the version that advancement
// produced. These three complete the declared matrix around it.
//
// Every case here reasons about the same two-part replay key the
// workflow_advancement_receipt table stores: the storage key (tenant,
// instance, node, attempt, expected_instance_version) and, inside the row,
// the full request digest plus the resulting instance version. A replay is
// admitted only when the storage key hits, the digest matches byte-for-byte
// and the instance is still at the receipt's resulting version. Anything
// else is CONFLICT_STALE_INSTANCE -- never a second application, and never a
// receipt shared between two different requests.

// countAdvancementReceipts counts the durable receipt rows for one instance.
// It reads through the superuser pgtest connection rather than the
// least-privilege app role because the point of the assertion is what exists
// in the table, independent of the tenant scope any particular caller set.
func countAdvancementReceipts(t *testing.T, db *pgtest.DB, tenantID, instanceID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `
		SELECT count(*) FROM workflow_advancement_receipt
		WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instanceID).Scan(&n); err != nil {
		t.Fatalf("count advancement receipts: %v", err)
	}
	return n
}

// storedInstanceVersion reads one instance's committed optimistic version.
func storedInstanceVersion(t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID) int64 {
	t.Helper()
	var version int64
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `
			SELECT instance_version FROM workflow_instance
			WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instanceID).Scan(&version)
	})
	return version
}

// snapshotAdvanceRequest is the first advancement of a freshly started
// promotion instance: the start node reporting SUCCEEDED. Every case in this
// file builds from it so that a mutation is always exactly one field of a
// request that would otherwise apply or replay.
func snapshotAdvanceRequest(
	tenantID, instanceID uuid.UUID, plan *workflow.CompiledWorkflow,
	expectedVersion int64, sink runtime.ContinuationSink, outputDigest string,
) runtime.AdvanceRequest {
	return runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: instanceID,
		ExpectedInstanceVersion: expectedVersion, Attempt: 1,
		Plan: plan,
		Outcome: frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded,
			OutputDigest: outputDigest,
		},
		Refs:       runtime.GovernanceRefs{CapabilityExecutionID: "capexec:wfrun031"},
		TraceID:    "trace:wfrun031",
		RecordedAt: fixedInstant, Sink: sink,
	}
}

// TestTodo_WF_RUN_031_Race submits one byte-identical advancement from six
// independent connections at once, then resubmits it once more serially.
//
// The receipt is the single replay authority, so the outcome must be: exactly
// one application, exactly one receipt row, one instance-version increment
// step, and a later serial resubmission recognized as a replay of that same
// receipt rather than a second advancement. A concurrent loser is refused
// CONFLICT_STALE_INSTANCE by the instance's own compare-and-set before it can
// reach the receipt insert; it is never allowed to "win" by inserting a
// second receipt under the same key.
func TestTodo_WF_RUN_031_Race(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun031-race")
	pf := newPromotionFixture(t, values.TenantId("wfrun031-race-tenant"), "intent:wf-run-031-race")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun031-race"))

	const workers = 6
	type outcome struct {
		receipt runtime.AdvanceReceipt
		err     error
	}
	results := make([]outcome, workers)
	conns := make([]*pgxadapter.Conn, workers)
	for i := range conns {
		conns[i] = appConn(t, db)
	}
	sink := runtime.NewMemorySink()
	req := snapshotAdvanceRequest(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion, sink, "sha256:wfrun031-race")

	var gate sync.WaitGroup
	var done sync.WaitGroup
	gate.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			gate.Wait()
			results[i].err = inTenantTxErr(conns[i], tenantID, func(tx dbport.Tx) error {
				var advErr error
				results[i].receipt, advErr = runtime.Advance(context.Background(), tx, req)
				return advErr
			})
		}(i)
	}
	gate.Done()
	done.Wait()

	applied := 0
	var winner runtime.AdvanceReceipt
	for i, res := range results {
		switch {
		case res.err == nil && !res.receipt.Replay:
			applied++
			winner = res.receipt
		case res.err == nil && res.receipt.Replay:
			// A caller that arrived after the winner committed and read the
			// receipt back: legal, and it must report the winner's own result.
		case runtime.CodeOf(res.err) == runtime.CodeStaleInstance:
			// A caller the compare-and-set overtook: legal, wrote nothing.
		default:
			t.Fatalf("concurrent Advance %d: unexpected %q (%v)", i, runtime.CodeOf(res.err), res.err)
		}
	}
	if applied != 1 {
		t.Fatalf("%d concurrent identical advancements applied, want exactly 1", applied)
	}
	for i, res := range results {
		if res.err == nil && res.receipt.Digest() != winner.Digest() {
			t.Fatalf("concurrent result %d has digest %s, want the single applied receipt's %s",
				i, res.receipt.Digest(), winner.Digest())
		}
	}
	if got := countAdvancementReceipts(t, db, tenantID, start.InstanceID); got != 1 {
		t.Fatalf("%d advancement receipt rows after the race, want 1", got)
	}
	if got := storedInstanceVersion(t, conn, tenantID, start.InstanceID); got != winner.NewInstanceVersion {
		t.Fatalf("instance version %d after the race, want the single applied receipt's %d",
			got, winner.NewInstanceVersion)
	}

	// A serial resubmission of the same request, with the instance still at
	// the receipt's resulting version, replays that receipt.
	replayed, err := advanceOnce(t, conn, tenantID, req)
	if err != nil {
		t.Fatalf("identical resubmission after the race: %v", err)
	}
	if !replayed.Replay || replayed.Digest() != winner.Digest() {
		t.Fatalf("resubmission replay=%v digest=%s, want replay=true digest=%s",
			replayed.Replay, replayed.Digest(), winner.Digest())
	}
	if got := countAdvancementReceipts(t, db, tenantID, start.InstanceID); got != 1 {
		t.Fatalf("%d advancement receipt rows after the replay, want 1", got)
	}

	// Two different requests at the same expected version never share a
	// receipt: the second is stale, not a replay of the first.
	other := snapshotAdvanceRequest(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion, sink, "sha256:wfrun031-race-other")
	if _, err := advanceOnce(t, conn, tenantID, other); runtime.CodeOf(err) != runtime.CodeStaleInstance {
		t.Fatalf("a different request at the same expected version: code = %q, want %q (%v)",
			runtime.CodeOf(err), runtime.CodeStaleInstance, err)
	}
	if got := storedInstanceVersion(t, conn, tenantID, start.InstanceID); got != winner.NewInstanceVersion {
		t.Fatalf("a refused different request moved the instance to version %d, want %d",
			got, winner.NewInstanceVersion)
	}
}

// TestTodo_WF_RUN_031_Fault proves the receipt is durable evidence and
// nothing else: an advancement whose transaction never committed leaves no
// receipt and no claim of progress, and a receipt row whose stored payload
// no longer digests to what it claims is refused rather than replayed.
func TestTodo_WF_RUN_031_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun031-fault")
	pf := newPromotionFixture(t, values.TenantId("wfrun031-fault-tenant"), "intent:wf-run-031-fault")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun031-fault"))
	sink := runtime.NewMemorySink()
	req := snapshotAdvanceRequest(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion, sink, "sha256:wfrun031-fault")

	t.Run("rolled_back_advancement_records_no_receipt", func(t *testing.T) {
		// Advance succeeds inside the transaction; the caller then rolls it
		// back. Nothing about that call may survive: no receipt, no version
		// bump, and the very same request must still apply freshly afterwards.
		rollbackErr := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			if _, err := runtime.Advance(context.Background(), tx, req); err != nil {
				return err
			}
			return errRollbackOnPurpose
		})
		if rollbackErr != errRollbackOnPurpose {
			t.Fatalf("deliberate rollback: %v", rollbackErr)
		}
		if got := countAdvancementReceipts(t, db, tenantID, start.InstanceID); got != 0 {
			t.Fatalf("%d receipt rows after a rolled-back advancement, want 0", got)
		}
		if got := storedInstanceVersion(t, conn, tenantID, start.InstanceID); got != start.InstanceVersion {
			t.Fatalf("instance version %d after a rolled-back advancement, want %d", got, start.InstanceVersion)
		}
	})

	applied, err := advanceOnce(t, conn, tenantID, req)
	if err != nil {
		t.Fatalf("Advance after the rollback: %v", err)
	}
	if applied.Replay {
		t.Fatal("the request after a rolled-back attempt replayed a receipt that was never committed")
	}

	t.Run("tampered_receipt_payload_is_refused_not_replayed", func(t *testing.T) {
		// The stored payload carries the receipt's own digest. Editing any
		// field of the payload without editing that digest -- the shape a
		// direct SQL edit of the evidence takes -- must refuse the replay
		// rather than hand the caller a receipt the runtime never produced.
		db.Exec(t, `
			UPDATE workflow_advancement_receipt
			SET receipt = jsonb_set(receipt, '{output_digest}', '"sha256:tampered"')
			WHERE tenant_id = $1 AND instance_id = $2`, tenantID, start.InstanceID)

		_, replayErr := advanceOnce(t, conn, tenantID, req)
		if runtime.CodeOf(replayErr) != runtime.CodeStorageFailed {
			t.Fatalf("replay of a tampered receipt: code = %q, want %q (%v)",
				runtime.CodeOf(replayErr), runtime.CodeStorageFailed, replayErr)
		}
		if got := storedInstanceVersion(t, conn, tenantID, start.InstanceID); got != applied.NewInstanceVersion {
			t.Fatalf("a refused replay moved the instance to version %d, want %d",
				got, applied.NewInstanceVersion)
		}
	})
}

// errRollbackOnPurpose is the sentinel [TestTodo_WF_RUN_031_Fault] returns to
// make [inTenantTxErr] roll back a transaction whose Advance succeeded.
var errRollbackOnPurpose = &rollbackSentinel{}

type rollbackSentinel struct{}

func (*rollbackSentinel) Error() string { return "wfrun031: deliberate rollback" }

// TestTodo_WF_RUN_031_Mutation kills one mutant per field of the advancement
// request digest. Each variant differs from the applied request in exactly
// one place, hits the same storage key (same tenant, instance, node, attempt
// and expected version) and must therefore be refused
// CONFLICT_STALE_INSTANCE: a request that is not byte-for-byte the recorded
// one is a different command, not a replay. The unmutated request still
// replays afterwards, which is what makes each refusal attributable to the
// mutation rather than to the instance having moved on.
func TestTodo_WF_RUN_031_Mutation(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun031-mutation")
	pf := newPromotionFixture(t, values.TenantId("wfrun031-mutation-tenant"), "intent:wf-run-031-mutation")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun031-mutation"))
	sink := runtime.NewMemorySink()
	req := snapshotAdvanceRequest(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion, sink, "sha256:wfrun031-mutation")

	applied, err := advanceOnce(t, conn, tenantID, req)
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}

	mutants := []struct {
		name  string
		apply func(*runtime.AdvanceRequest)
	}{
		{"output_digest", func(r *runtime.AdvanceRequest) { r.Outcome.OutputDigest = "sha256:wfrun031-mutant" }},
		{"trace_id", func(r *runtime.AdvanceRequest) { r.TraceID = "trace:wfrun031-mutant" }},
		{"governance_refs", func(r *runtime.AdvanceRequest) {
			r.Refs.CapabilityExecutionID = "capexec:wfrun031-mutant"
		}},
		{"recorded_at", func(r *runtime.AdvanceRequest) { r.RecordedAt = fixedInstant.Add(1) }},
		{"error_class", func(r *runtime.AdvanceRequest) { r.Outcome.ErrorClass = "TRANSIENT" }},
	}
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {
			mutated := req
			m.apply(&mutated)
			_, mutErr := advanceOnce(t, conn, tenantID, mutated)
			if runtime.CodeOf(mutErr) != runtime.CodeStaleInstance {
				t.Fatalf("mutated %s: code = %q, want %q (%v)",
					m.name, runtime.CodeOf(mutErr), runtime.CodeStaleInstance, mutErr)
			}
			if got := storedInstanceVersion(t, conn, tenantID, start.InstanceID); got != applied.NewInstanceVersion {
				t.Fatalf("mutated %s moved the instance to version %d, want %d",
					m.name, got, applied.NewInstanceVersion)
			}
			if got := countAdvancementReceipts(t, db, tenantID, start.InstanceID); got != 1 {
				t.Fatalf("mutated %s left %d receipt rows, want 1", m.name, got)
			}
		})
	}

	replayed, err := advanceOnce(t, conn, tenantID, req)
	if err != nil {
		t.Fatalf("unmutated resubmission after every mutant: %v", err)
	}
	if !replayed.Replay || replayed.Digest() != applied.Digest() {
		t.Fatalf("unmutated resubmission replay=%v digest=%s, want replay=true digest=%s",
			replayed.Replay, replayed.Digest(), applied.Digest())
	}
}
