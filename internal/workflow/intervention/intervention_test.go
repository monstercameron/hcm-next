package intervention

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var interventionInstant = time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)

func interventionPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	plan, err := workflow.CompilePromotionReference(registry)
	if err != nil {
		t.Fatalf("compile promotion plan: %v", err)
	}
	return plan
}

func interventionRequest(t *testing.T, kind Kind) Request {
	t.Helper()
	plan := interventionPlan(t)
	return Request{
		WorkflowID: workflow.PromotionWorkflowID, WorkflowVersion: workflow.PromotionVersion, CompiledPlanDigest: plan.Digest(),
		InstanceID: "instance-1", ExpectedVersion: 8, Kind: kind, StepID: workflow.PromotionNodeEvaluateBand,
		Frontier: []string{workflow.PromotionNodeEvaluateBand}, Plan: plan, Reason: "INCIDENT_REVIEW", EvidenceRef: "incident:123",
		RequestedBy: "principal:operator", ApprovedBy: "principal:approver", Assignee: "principal:new-owner",
		IdempotencyKey: "intervention:1", RequestedAt: interventionInstant,
	}
}

func TestTodo_WF_RUN_015(t *testing.T) {
	cases := []struct {
		kind               Kind
		instanceTo, nodeTo string
	}{
		{Pause, "PAUSE_REQUESTED", ""}, {Resume, "RUNNING", ""}, {SkipStep, "", "SKIPPED"},
		{RetryStep, "", "READY"}, {Reassign, "", "ASSIGNED"}, {Cancel, "CANCELLING", ""},
		{ForceCompleteWithEvidence, "COMPLETED", ""},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			req := interventionRequest(t, tc.kind)
			if !tc.kind.RequiresApproval() {
				req.ApprovedBy = ""
			}
			receipt, err := Evaluate(req)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if receipt.Decision != DecisionAccepted || receipt.Transition.InstanceTo != tc.instanceTo || receipt.Transition.NodeTo != tc.nodeTo || receipt.Digest() == "" || receipt.Event.Digest() == "" {
				t.Fatalf("receipt = %+v", receipt)
			}
			if err := receipt.Verify(); err != nil {
				t.Fatalf("verify receipt: %v", err)
			}
		})
	}
}

func TestTodo_WF_RUN_015_Race(t *testing.T) {
	req := interventionRequest(t, Pause)
	const workers = 24
	results := make([]Receipt, workers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], _ = Evaluate(req) }(i)
	}
	wg.Wait()
	for i := 1; i < len(results); i++ {
		if results[i].Digest() != results[0].Digest() || results[i].Event.Digest() != results[0].Event.Digest() {
			t.Fatalf("receipt %d differs", i)
		}
	}
}

func TestTodo_WF_RUN_015_Fault(t *testing.T) {
	unknown := interventionRequest(t, Kind("FORCE_WORKFLOW"))
	if _, err := Evaluate(unknown); CodeOf(err) != CodeUnsupportedKind || !errors.Is(err, ErrIntervention) {
		t.Fatalf("unknown kind error = %v", err)
	}
	unsafe := interventionRequest(t, Pause)
	unsafe.Plan.Concurrency = &workflow.ConcurrencySummary{InterventionIneligibleNodes: []string{workflow.PromotionNodeEvaluateBand}}
	if _, err := Evaluate(unsafe); CodeOf(err) != CodeIneligibleRegion {
		t.Fatalf("unsafe-region error = %v", err)
	}
	missingApproval := interventionRequest(t, Cancel)
	missingApproval.ApprovedBy = ""
	if _, err := Evaluate(missingApproval); CodeOf(err) != CodeApprovalRequired {
		t.Fatalf("missing approval error = %v", err)
	}
	wrongStatus := interventionRequest(t, Resume)
	wrongStatus.ApprovedBy = ""
	wrongStatus.CurrentStatus = "RUNNING"
	if _, err := Evaluate(wrongStatus); CodeOf(err) != CodeIllegalPrecondition {
		t.Fatalf("wrong-status error = %v", err)
	}
}

func TestTodo_WF_RUN_015_Mutation(t *testing.T) {
	receipt, err := Evaluate(interventionRequest(t, Pause))
	if err != nil {
		t.Fatal(err)
	}
	receipt.Reason = "tampered"
	if CodeOf(receipt.Verify()) != CodeReceiptMutated {
		t.Fatalf("tampered receipt was accepted")
	}
	receipt, err = Evaluate(interventionRequest(t, Pause))
	if err != nil {
		t.Fatal(err)
	}
	receipt.Event.ReceiptDigest = "tampered"
	if CodeOf(receipt.Verify()) != CodeEventMutated {
		t.Fatalf("tampered event was accepted")
	}
}
