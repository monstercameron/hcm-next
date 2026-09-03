package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/domains/promotion"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/prototype"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// stubBeginner is a non-nil placeholder satisfying execute.Beginner.
// NewPromotionExecution only validates that a Beginner exists; it never calls
// Begin at construction time, so the stub refuses to be used.
type stubBeginner struct{}

func (stubBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("stubBeginner: Begin must not be called at construction")
}

// stubTerminal is a non-nil placeholder satisfying execute.TerminalWriter,
// for the same reason as stubBeginner.
type stubTerminal struct{}

func (stubTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return idempotency.ResultIdentity{}, errors.New("stubTerminal: Write must not be called at construction")
}

// TestNewPromotionExecutionValidation proves the two required ports are
// enforced before any composition happens at all.
func TestNewPromotionExecutionValidation(t *testing.T) {
	if _, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: stubBeginner{}, Terminal: stubTerminal{},
	}); err != nil {
		// sanity: the happy path below depends on these stubs being accepted
		t.Fatalf("NewPromotionExecution with a stub DB/Terminal: %v", err)
	}
	if _, err := NewPromotionExecution(PromotionExecutionConfig{Terminal: stubTerminal{}}); err == nil {
		t.Fatal("NewPromotionExecution without a DB Beginner: expected an error")
	}
	if _, err := NewPromotionExecution(PromotionExecutionConfig{DB: stubBeginner{}}); err == nil {
		t.Fatal("NewPromotionExecution without a TerminalWriter: expected an error")
	}
}

// TestNewPromotionExecutionDefaults proves the composition root's asserted
// P1B authority and its default configuration land where the docs say they
// land: the closed intent-type set, the required role (default and override),
// the carried-through authority digest, and a ready-but-empty evidence sink.
func TestNewPromotionExecutionDefaults(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }
	base := PromotionExecutionConfig{DB: stubBeginner{}, Terminal: stubTerminal{}, Clock: clock}

	custom, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: stubBeginner{}, Terminal: stubTerminal{}, Clock: clock,
		AuthorityDigest: "sha256:customauthoritydigest", ApproverPrincipalID: "principal:custom",
		RequiredRole: "custom_operator",
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}

	baseGet, err := NewPromotionExecution(base)
	if err != nil {
		t.Fatalf("NewPromotionExecution (defaults): %v", err)
	}

	if got := baseGet.Authority.RequiredRole; got != "promotion_operator" {
		t.Errorf("default RequiredRole = %q, want %q", got, "promotion_operator")
	}
	if got := custom.Authority.RequiredRole; got != "custom_operator" {
		t.Errorf("override RequiredRole = %q, want %q", got, "custom_operator")
	}
	if len(baseGet.Authority.AdmittedIntentTypes) != 1 || !baseGet.Authority.AdmittedIntentTypes[promotion.IntentType] {
		t.Errorf("AdmittedIntentTypes = %#v, want exactly {%s: true}", baseGet.Authority.AdmittedIntentTypes, promotion.IntentType)
	}
	if custom.Authority.AuthorityDigest != "sha256:customauthoritydigest" {
		t.Errorf("AuthorityDigest = %q, want it carried through unchanged", custom.Authority.AuthorityDigest)
	}

	for name, ex := range map[string]*PromotionExecution{"defaults": baseGet, "custom": custom} {
		if ex.Executor == nil {
			t.Errorf("%s: Executor is nil", name)
		}
		if ex.Resolver == nil {
			t.Errorf("%s: Resolver is nil", name)
		}
		if ex.Versions == nil {
			t.Errorf("%s: Versions is nil", name)
		}
		if ex.Evidence == nil {
			t.Fatalf("%s: Evidence is nil", name)
		}
		if n := ex.Evidence.Len(); n != 0 {
			t.Errorf("%s: Evidence.Len() = %d at construction, want 0", name, n)
		}
	}
}

// TestPromotionStepRunner proves the two bounded node types the composed
// promote_worker graph uses: APPROVAL parks on exactly the one approval
// requirement the prototype plan names, END completes bare, and any other
// node type is a composition error, not a silent no-op.
func TestPromotionStepRunner(t *testing.T) {
	runner := promotionStepRunner{}
	ctx := context.Background()

	t.Run("approval parks on the prototype requirement", func(t *testing.T) {
		outcome, refs, err := runner.Run(ctx, execute.StepRequest{
			Node: workflow.CompiledNode{ID: "approve", Type: workflow.StepApproval},
		})
		if err != nil {
			t.Fatalf("Run(APPROVAL): %v", err)
		}
		if outcome.NodeID != "approve" {
			t.Errorf("NodeID = %q, want approve", outcome.NodeID)
		}
		if outcome.Await != frontier.AwaitWorkItem {
			t.Errorf("Await = %q, want %q", outcome.Await, frontier.AwaitWorkItem)
		}
		if outcome.AwaitRef != prototype.ApprovalRequirementID {
			t.Errorf("AwaitRef = %q, want %q", outcome.AwaitRef, prototype.ApprovalRequirementID)
		}
		if outcome.Outcome != "" {
			t.Errorf("Outcome = %q at an APPROVAL, want empty", outcome.Outcome)
		}
		if len(refs.EffectRefs) != 0 || refs.ProposalRef != "" || refs.PolicyRef != "" || refs.HumanTaskID != "" {
			t.Errorf("GovernanceRefs = %#v at an APPROVAL, want zero", refs)
		}
	})

	t.Run("end completes bare", func(t *testing.T) {
		outcome, _, err := runner.Run(ctx, execute.StepRequest{
			Node: workflow.CompiledNode{ID: "finish", Type: workflow.StepEnd},
		})
		if err != nil {
			t.Fatalf("Run(END): %v", err)
		}
		if outcome.NodeID != "finish" {
			t.Errorf("NodeID = %q, want finish", outcome.NodeID)
		}
		if outcome.Await != "" || outcome.AwaitRef != "" {
			t.Errorf("END outcome must carry no await, got Await=%q AwaitRef=%q", outcome.Await, outcome.AwaitRef)
		}
	})

	t.Run("anything else is a composition error", func(t *testing.T) {
		for _, nodeType := range []workflow.StepType{
			workflow.StepCapability, workflow.StepTask, workflow.StepWait, workflow.StepSignal,
		} {
			if _, _, err := runner.Run(ctx, execute.StepRequest{
				Node: workflow.CompiledNode{ID: "other", Type: nodeType},
			}); err == nil {
				t.Errorf("Run(%s): expected an error for an uncomposable node type", nodeType)
			}
		}
	})
}

// TestAdaptExecutionResult proves the projection from execute.Driver's own
// Result onto the port-owned app.ExecutionResult: visited nodes in order, the
// deprecated work-item named ParkedContinuations, the correctly-named typed
// lists (WF-RUN-032), continuation filtering down to the kinds that actually
// park (READY and COMPLETE excluded), and an EvidenceIDs copy that cannot be
// corrupted by the driver's own retry logic.
func TestAdaptExecutionResult(t *testing.T) {
	tenant := uuid.New()
	instance := uuid.New()
	itemID := uuid.New()

	workItemReq := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "request", SourceAttempt: 1, TargetNodeID: "approve",
		Kind: frontier.IntentWorkItemRequired,
	}
	signalReq := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "request", SourceAttempt: 1, TargetNodeID: "observe",
		Kind: frontier.IntentSignalSubscriptionRequired,
	}
	readyWork := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "request", SourceAttempt: 1,
		Kind: frontier.IntentReady,
	}
	completeRecord := runtime.ContinuationRecord{
		TenantID: tenant, InstanceID: instance,
		SourceNodeID: "finish", SourceAttempt: 2, TargetNodeID: "finish",
		Kind: frontier.IntentComplete,
	}

	result := execute.Result{
		Status: execute.StatusParked, InstanceVersion: 3,
		Advances: []runtime.AdvanceReceipt{
			{
				NodeID: "request",
				Continuations: []runtime.ContinuationRecord{
					workItemReq, signalReq, readyWork,
				},
			},
			{NodeID: "finish", Continuations: []runtime.ContinuationRecord{completeRecord}},
		},
		WorkItems: []workitem.WorkItem{{
			WorkItemID: itemID, Kind: workitem.KindApproval,
			WorkType: prototype.ApprovalRequirementID, NodeID: "approve",
		}},
		EvidenceIDs: []string{"obs-024-1", "obs-024-2"},
	}

	got := adaptExecutionResult(result, instance.String())

	if !got.Parked {
		t.Error("Parked = false, want true for a PARKED driver result")
	}
	if got.InstanceID != instance.String() {
		t.Errorf("InstanceID = %q, want the passed string form %q", got.InstanceID, instance.String())
	}
	if got.InstanceVersion != 3 {
		t.Errorf("InstanceVersion = %d, want 3", got.InstanceVersion)
	}
	if len(got.VisitedNodes) != 2 || got.VisitedNodes[0] != "request" || got.VisitedNodes[1] != "finish" {
		t.Errorf("VisitedNodes = %#v, want [request finish] in order", got.VisitedNodes)
	}

	// The deprecated field keeps its historical "<work_type>:<work_item_id>"
	// naming for every raised item.
	wantDeprecated := prototype.ApprovalRequirementID + ":" + itemID.String()
	if len(got.ParkedContinuations) != 1 || got.ParkedContinuations[0] != wantDeprecated {
		t.Errorf("ParkedContinuations = %#v, want [%s]", got.ParkedContinuations, wantDeprecated)
	}

	// The typed refs carry only the genuinely-parking kinds, with the exact
	// runtime-derived continuation identity.
	wantWorkItemRef := runtime.ContinuationID(workItemReq.TenantID, workItemReq.InstanceID,
		workItemReq.SourceNodeID, workItemReq.SourceAttempt, workItemReq.TargetNodeID, workItemReq.Kind).String()
	wantSignalRef := runtime.ContinuationID(signalReq.TenantID, signalReq.InstanceID,
		signalReq.SourceNodeID, signalReq.SourceAttempt, signalReq.TargetNodeID, signalReq.Kind).String()
	wantRef := []app.ContinuationRef{
		{ContinuationID: wantWorkItemRef, Kind: "WORK_ITEM_REQUIRED", TargetNodeID: "approve"},
		{ContinuationID: wantSignalRef, Kind: "SIGNAL_SUBSCRIPTION_REQUIRED", TargetNodeID: "observe"},
	}
	if len(got.ParkedContinuationRefs) != 2 {
		t.Fatalf("ParkedContinuationRefs = %#v, want exactly 2 (READY and COMPLETE filtered out)", got.ParkedContinuationRefs)
	}
	for i, want := range wantRef {
		if got.ParkedContinuationRefs[i] != want {
			t.Errorf("ParkedContinuationRefs[%d] = %#v, want %#v", i, got.ParkedContinuationRefs[i], want)
		}
	}

	wantItems := []app.WorkItemRef{{WorkItemID: itemID.String(), Kind: string(workitem.KindApproval), NodeID: "approve"}}
	if len(got.ParkedWorkItems) != 1 || got.ParkedWorkItems[0] != wantItems[0] {
		t.Errorf("ParkedWorkItems = %#v, want %#v", got.ParkedWorkItems, wantItems)
	}

	if len(got.EvidenceIDs) != 2 || got.EvidenceIDs[0] != "obs-024-1" || got.EvidenceIDs[1] != "obs-024-2" {
		t.Errorf("EvidenceIDs = %#v, want [obs-024-1 obs-024-2]", got.EvidenceIDs)
	}
	result.EvidenceIDs = append(result.EvidenceIDs, "corrupted-after-adapt")
	if len(got.EvidenceIDs) != 2 {
		t.Errorf("EvidencedIDs changed after mutating the source: %#v", got.EvidenceIDs)
	}

	// A COMPLETE result parks on nothing.
	complete := execute.Result{Status: execute.StatusComplete, InstanceVersion: 7}
	completeGot := adaptExecutionResult(complete, instance.String())
	if completeGot.Parked || len(completeGot.ParkedContinuationRefs) != 0 || len(completeGot.ParkedWorkItems) != 0 {
		t.Errorf("complete result must project to an unparked result, got %#v", completeGot)
	}
	if completeGot.InstanceVersion != 7 {
		t.Errorf("complete InstanceVersion = %d, want 7", completeGot.InstanceVersion)
	}
}
