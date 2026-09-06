package runtime_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"

	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// --- WF-RUN-008 ------------------------------------------------------------
//
// Pause is caller-driven end to end. A request is recorded as
// PAUSE_REQUESTED and becomes PAUSED only at an eligible safe point, and the
// only thing that ever evaluates eligibility is one of the caller's own calls
// -- RequestPause, ApplyPause, or the gate Advance runs. There is no
// scheduler, no lease, no timer and no background worker anywhere in this
// path, which is exactly the envelope WF-RUN-000's prototype ruling admits.
//
// Eligibility has two halves. The durable half -- "no node execution is
// RUNNING", the never-mid-node rule -- is exercised here against the real
// promotion plan. The compiled half -- "no frontier node is inside a
// WF-COMP-004 atomic region" -- is exercised in internal/workflow's
// TestTodo_WF_COMP_004_Mutation, because the promotion reference is
// zero-effect and therefore declares no atomic region to stand inside.

// pauseRequestFor builds a fully valid [runtime.PauseRequest] against one
// started instance, so every RED case mutates exactly one field of a request
// that would otherwise succeed.
func pauseRequestFor(
	tenantID, instanceID uuid.UUID, plan *workflow.CompiledWorkflow, expectedVersion int64,
) runtime.PauseRequest {
	return runtime.PauseRequest{
		TenantID: tenantID, InstanceID: instanceID,
		ExpectedInstanceVersion: expectedVersion,
		Plan:                    plan,
		Reason:                  "INCIDENT_REVIEW",
		RequestedBy:             "principal:operations-duty",
		RequestedAt:             fixedInstant,
	}
}

func requestPause(
	t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.PauseRequest,
) (runtime.PauseReceipt, error) {
	t.Helper()
	var receipt runtime.PauseReceipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var pauseErr error
		receipt, pauseErr = runtime.RequestPause(context.Background(), tx, req)
		return pauseErr
	})
	return receipt, err
}

func applyPause(
	t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.PauseRequest,
) (runtime.PauseReceipt, error) {
	t.Helper()
	var receipt runtime.PauseReceipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var pauseErr error
		receipt, pauseErr = runtime.ApplyPause(context.Background(), tx, req)
		return pauseErr
	})
	return receipt, err
}

func resumeFromPause(
	t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.ResumeRequest,
) (runtime.PauseReceipt, error) {
	t.Helper()
	var receipt runtime.PauseReceipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var resumeErr error
		receipt, resumeErr = runtime.ResumeFromPause(context.Background(), tx, req)
		return resumeErr
	})
	return receipt, err
}

// loadInstanceForTest reads one instance back through the app role.
func loadInstanceForTest(
	t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID,
) runtime.Instance {
	t.Helper()
	var inst runtime.Instance
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		inst, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, instanceID)
		return err
	})
	return inst
}

// markNodeRunning records the start node as RUNNING, which is what a caller's
// step handler having entered the node looks like durably. It is this
// package's only "we are mid-node" fact, and the whole never-mid-node rule
// hangs off it.
func markNodeRunning(
	t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID, nodeID string, expectedVersion int64,
) int64 {
	t.Helper()
	var next int64
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, bumped, err := (runtime.Store{}).RecordNodeTransition(context.Background(), tx, runtime.NodeTransition{
			TenantID: tenantID, InstanceID: instanceID, NodeID: nodeID, Attempt: 1,
			ExpectedInstanceVersion: expectedVersion,
			Status:                  runtime.NodeRunning,
		})
		next = bumped
		return err
	})
	return next
}

// TestTodo_WF_RUN_008 is the PRIMARY test: a pause requested while the
// instance sits at a safe point takes effect immediately, a paused instance
// refuses to advance, and a resume revalidates the context before the
// instance runs again.
func TestTodo_WF_RUN_008(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun008")
	pf := newPromotionFixture(t, values.TenantId("wfrun008-tenant"), "intent:wf-run-008")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun008"))

	paused, err := requestPause(t, conn, tenantID,
		pauseRequestFor(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion))
	if err != nil {
		t.Fatalf("RequestPause: %v", err)
	}
	if paused.Status != runtime.InstancePaused || !paused.Eligible || paused.BlockingNodeID != "" {
		t.Fatalf("pause at a safe point produced status=%s eligible=%v blocking=%q, want PAUSED/true/\"\"",
			paused.Status, paused.Eligible, paused.BlockingNodeID)
	}
	if inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID); inst.RuntimeStatus != runtime.InstancePaused {
		t.Fatalf("stored status = %s, want PAUSED", inst.RuntimeStatus)
	}

	// A paused instance refuses to advance. This is the whole point: the
	// pause is not advisory.
	sink := runtime.NewMemorySink()
	advReq := snapshotAdvanceRequest(tenantID, start.InstanceID, pf.Plan, paused.InstanceVersion, sink, "sha256:wfrun008")
	if _, advErr := advanceOnce(t, conn, tenantID, advReq); runtime.CodeOf(advErr) != runtime.CodeInstancePaused {
		t.Fatalf("Advance on a paused instance: code = %q, want %q (%v)",
			runtime.CodeOf(advErr), runtime.CodeInstancePaused, advErr)
	}

	resumed, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
		TenantID: tenantID, InstanceID: start.InstanceID,
		ExpectedInstanceVersion: paused.InstanceVersion,
		Plan:                    pf.Plan,
		ResolvedContext:         promotionResolvedContext(),
		Reason:                  "INCIDENT_CLOSED",
		ResumedBy:               "principal:operations-duty",
		ResumedAt:               fixedInstant,
	})
	if err != nil {
		t.Fatalf("ResumeFromPause: %v", err)
	}
	if resumed.Status != runtime.InstanceRunning {
		t.Fatalf("resumed status = %s, want RUNNING", resumed.Status)
	}

	advReq.ExpectedInstanceVersion = resumed.InstanceVersion
	if _, advErr := advanceOnce(t, conn, tenantID, advReq); advErr != nil {
		t.Fatalf("Advance after a resume: %v", advErr)
	}
}

// TestTodo_WF_RUN_008_Golden pins the receipt a pause produces: the exact
// status, eligibility, frontier and compiled safe-point list an operator
// reads, and a content digest that is a function of that content rather than
// of when the receipt was produced.
func TestTodo_WF_RUN_008_Golden(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun008-golden")
	pf := newPromotionFixture(t, values.TenantId("wfrun008-golden-tenant"), "intent:wf-run-008-golden")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun008-golden"))

	receipt, err := requestPause(t, conn, tenantID,
		pauseRequestFor(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion))
	if err != nil {
		t.Fatalf("RequestPause: %v", err)
	}

	// The safe points on the receipt are exactly the plan's compiled ones --
	// the operator does not get a second, differently-derived list.
	wantSafePoints := map[string]bool{}
	for _, n := range pf.Plan.Nodes {
		if n.SafePoint {
			wantSafePoints[n.ID] = true
		}
	}
	if len(receipt.SafePoints) != len(wantSafePoints) {
		t.Fatalf("receipt names %d safe points, the plan compiles %d: %v",
			len(receipt.SafePoints), len(wantSafePoints), receipt.SafePoints)
	}
	for _, id := range receipt.SafePoints {
		if !wantSafePoints[id] {
			t.Fatalf("receipt names %q as a safe point; the compiled plan does not", id)
		}
	}
	if len(receipt.Frontier) != 1 || receipt.Frontier[0] != pf.Plan.StartNodeID {
		t.Fatalf("paused frontier = %v, want [%s]", receipt.Frontier, pf.Plan.StartNodeID)
	}
	if receipt.Reason != "INCIDENT_REVIEW" || receipt.Actor != "principal:operations-duty" {
		t.Fatalf("receipt lost the governed reason/actor: %q by %q", receipt.Reason, receipt.Actor)
	}
	if receipt.Digest() == "" {
		t.Fatal("pause receipt carries no content digest")
	}

	// Re-reading the same paused instance reproduces the same digest: the
	// receipt's identity is its content, not the call that produced it.
	again, err := requestPause(t, conn, tenantID,
		pauseRequestFor(tenantID, start.InstanceID, pf.Plan, receipt.InstanceVersion))
	if err != nil {
		t.Fatalf("second RequestPause: %v", err)
	}
	if !again.Replay {
		t.Fatal("a second pause request against an already paused instance is not reported as a replay")
	}
	if again.Digest() != receipt.Digest() {
		t.Fatalf("replayed receipt digests to %s, want %s", again.Digest(), receipt.Digest())
	}
}

// TestTodo_WF_RUN_008_Race fires many pause requests at one instance from
// independent connections. Pause is a state transition fenced by the same
// optimistic instance version as every other write here, so the outcome must
// be one instance, one PAUSED status, and no caller left believing it paused
// something it did not.
func TestTodo_WF_RUN_008_Race(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun008-race")
	pf := newPromotionFixture(t, values.TenantId("wfrun008-race-tenant"), "intent:wf-run-008-race")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun008-race"))

	const workers = 6
	type outcome struct {
		receipt runtime.PauseReceipt
		err     error
	}
	results := make([]outcome, workers)
	conns := make([]*pgxadapter.Conn, workers)
	for i := range conns {
		conns[i] = appConn(t, db)
	}
	req := pauseRequestFor(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion)

	var gate sync.WaitGroup
	var done sync.WaitGroup
	gate.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			gate.Wait()
			results[i].err = inTenantTxErr(conns[i], tenantID, func(tx dbport.Tx) error {
				var pauseErr error
				results[i].receipt, pauseErr = runtime.RequestPause(context.Background(), tx, req)
				return pauseErr
			})
		}(i)
	}
	gate.Done()
	done.Wait()

	applied := 0
	for i, res := range results {
		switch {
		case res.err == nil && !res.receipt.Replay:
			if res.receipt.Status != runtime.InstancePaused {
				t.Fatalf("concurrent pause %d applied but reported status %s, want PAUSED", i, res.receipt.Status)
			}
			applied++
		case res.err == nil && res.receipt.Replay:
			if res.receipt.Status != runtime.InstancePaused {
				t.Fatalf("concurrent pause %d replayed status %s, want PAUSED", i, res.receipt.Status)
			}
		case runtime.CodeOf(res.err) == runtime.CodeStaleInstance:
			// Overtaken by the winner's own version bump: legal, wrote nothing.
		default:
			t.Fatalf("concurrent pause %d: unexpected %q (%v)", i, runtime.CodeOf(res.err), res.err)
		}
	}
	if applied != 1 {
		t.Fatalf("%d concurrent pause requests applied a transition, want exactly 1", applied)
	}
	if inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID); inst.RuntimeStatus != runtime.InstancePaused {
		t.Fatalf("stored status after the race = %s, want PAUSED", inst.RuntimeStatus)
	}
}

// TestTodo_WF_RUN_008_Fault is the RED clause: a pause requested while the
// instance is mid-node reports PAUSE_REQUESTED and names what is blocking it,
// never PAUSED, and never abandons the node in flight. The advancement that
// finishes the node still runs, the request survives it, and the pause takes
// effect at the boundary that follows.
func TestTodo_WF_RUN_008_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun008-fault")
	pf := newPromotionFixture(t, values.TenantId("wfrun008-fault-tenant"), "intent:wf-run-008-fault")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun008-fault"))

	version := markNodeRunning(t, conn, tenantID, start.InstanceID, workflow.PromotionNodeSnapshotWorker, start.InstanceVersion)

	requested, err := requestPause(t, conn, tenantID,
		pauseRequestFor(tenantID, start.InstanceID, pf.Plan, version))
	if err != nil {
		t.Fatalf("RequestPause mid-node: %v", err)
	}
	if requested.Status != runtime.InstancePauseRequested {
		t.Fatalf("pause mid-node produced status %s, want PAUSE_REQUESTED", requested.Status)
	}
	if requested.Eligible || requested.BlockingNodeID != workflow.PromotionNodeSnapshotWorker {
		t.Fatalf("pause mid-node reported eligible=%v blocking=%q, want false/%s",
			requested.Eligible, requested.BlockingNodeID, workflow.PromotionNodeSnapshotWorker)
	}

	// The node in flight still finishes: a pause request never abandons an
	// active step.
	sink := runtime.NewMemorySink()
	advanced, err := advanceOnce(t, conn, tenantID, snapshotAdvanceRequest(
		tenantID, start.InstanceID, pf.Plan, requested.InstanceVersion, sink, "sha256:wfrun008-fault"))
	if err != nil {
		t.Fatalf("Advance of the in-flight node under a standing pause request: %v", err)
	}

	// And the request survived that advancement rather than being reset to
	// RUNNING by it.
	inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
	if inst.RuntimeStatus != runtime.InstancePauseRequested {
		t.Fatalf("status after advancing under a pause request = %s, want PAUSE_REQUESTED", inst.RuntimeStatus)
	}

	applied, err := applyPause(t, conn, tenantID,
		pauseRequestFor(tenantID, start.InstanceID, pf.Plan, advanced.NewInstanceVersion))
	if err != nil {
		t.Fatalf("ApplyPause at the next boundary: %v", err)
	}
	if applied.Status != runtime.InstancePaused || !applied.Eligible {
		t.Fatalf("ApplyPause at a safe point produced status=%s eligible=%v, want PAUSED/true",
			applied.Status, applied.Eligible)
	}

	t.Run("resume_of_an_unpaused_instance_is_refused", func(t *testing.T) {
		resumed, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
			TenantID: tenantID, InstanceID: start.InstanceID,
			ExpectedInstanceVersion: applied.InstanceVersion,
			Plan:                    pf.Plan,
			ResolvedContext:         promotionResolvedContext(),
			ResumedBy:               "principal:operations-duty",
			ResumedAt:               fixedInstant,
		})
		if err != nil {
			t.Fatalf("first ResumeFromPause: %v", err)
		}
		_, secondErr := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
			TenantID: tenantID, InstanceID: start.InstanceID,
			ExpectedInstanceVersion: resumed.InstanceVersion,
			Plan:                    pf.Plan,
			ResolvedContext:         promotionResolvedContext(),
			ResumedBy:               "principal:operations-duty",
			ResumedAt:               fixedInstant,
		})
		if runtime.CodeOf(secondErr) != runtime.CodeNotPaused {
			t.Fatalf("resuming an already-running instance: code = %q, want %q (%v)",
				runtime.CodeOf(secondErr), runtime.CodeNotPaused, secondErr)
		}
	})
}

// TestTodo_WF_RUN_008_Mutation kills one mutant per rule this pause path
// declares: the version fence, the pinned-plan check, the resume-time context
// revalidation, and ApplyPause's refusal to invent a pause nobody requested.
func TestTodo_WF_RUN_008_Mutation(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun008-mutation")
	pf := newPromotionFixture(t, values.TenantId("wfrun008-mutation-tenant"), "intent:wf-run-008-mutation")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-wfrun008-mutation"))

	t.Run("apply_pause_never_pauses_an_unrequested_instance", func(t *testing.T) {
		receipt, err := applyPause(t, conn, tenantID,
			pauseRequestFor(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion))
		if err != nil {
			t.Fatalf("ApplyPause with no standing request: %v", err)
		}
		if receipt.Status == runtime.InstancePaused || !receipt.Replay {
			t.Fatalf("ApplyPause invented a pause: status=%s replay=%v", receipt.Status, receipt.Replay)
		}
		if inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID); inst.InstanceVersion != start.InstanceVersion {
			t.Fatalf("ApplyPause with nothing to apply wrote: version %d, want %d",
				inst.InstanceVersion, start.InstanceVersion)
		}
	})

	t.Run("a_stale_version_pauses_nothing", func(t *testing.T) {
		req := pauseRequestFor(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion+7)
		_, err := requestPause(t, conn, tenantID, req)
		if runtime.CodeOf(err) != runtime.CodeStaleInstance {
			t.Fatalf("stale pause request: code = %q, want %q (%v)",
				runtime.CodeOf(err), runtime.CodeStaleInstance, err)
		}
		if inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID); inst.RuntimeStatus == runtime.InstancePaused {
			t.Fatal("a stale pause request paused the instance")
		}
	})

	t.Run("a_pause_reads_safe_points_from_the_pinned_plan_only", func(t *testing.T) {
		// A second, genuinely different compiled plan: the same promotion
		// definition published as version 2. Safe points are compiled facts
		// of one exact plan, so reading them out of another one is refused
		// rather than approximated.
		def := workflow.PromotionReferenceDefinition()
		def.Version = 2
		otherPlan, compileErr := workflow.Compile(def, workflow.Options{
			Phase: workflow.PhaseP1A, Capabilities: pf.Setup.Options.Capabilities,
		})
		if compileErr != nil {
			t.Fatalf("compile a second promotion version: %v", compileErr)
		}
		if otherPlan.Digest() == pf.Plan.Digest() {
			t.Fatal("the second version compiled to the same digest; this mutant proves nothing")
		}
		req := pauseRequestFor(tenantID, start.InstanceID, otherPlan, start.InstanceVersion)
		_, err := requestPause(t, conn, tenantID, req)
		if runtime.CodeOf(err) != runtime.CodeAdvancePlanMismatch {
			t.Fatalf("pause against another plan: code = %q, want %q (%v)",
				runtime.CodeOf(err), runtime.CodeAdvancePlanMismatch, err)
		}
	})

	t.Run("a_resume_without_todays_context_is_refused", func(t *testing.T) {
		paused, err := requestPause(t, conn, tenantID,
			pauseRequestFor(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion))
		if err != nil {
			t.Fatalf("RequestPause: %v", err)
		}
		if paused.Status != runtime.InstancePaused {
			t.Fatalf("status = %s, want PAUSED", paused.Status)
		}
		_, resumeErr := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
			TenantID: tenantID, InstanceID: start.InstanceID,
			ExpectedInstanceVersion: paused.InstanceVersion,
			Plan:                    pf.Plan,
			// No ResolvedContext at all: the start node declares a
			// LegalContext requirement, and a resume that cannot name today's
			// resolution must not run.
			ResumedBy: "principal:operations-duty",
			ResumedAt: fixedInstant,
		})
		if runtime.CodeOf(resumeErr) != runtime.CodeUnresolvedContext {
			t.Fatalf("resume with no revalidated context: code = %q, want %q (%v)",
				runtime.CodeOf(resumeErr), runtime.CodeUnresolvedContext, resumeErr)
		}
		if inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID); inst.RuntimeStatus != runtime.InstancePaused {
			t.Fatalf("a refused resume left the instance %s, want PAUSED", inst.RuntimeStatus)
		}
	})
}
