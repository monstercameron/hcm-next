package app

// PROMOUX-003: "Enforce and explain separation of duties across promotion
// approvals." This file exercises the two pieces of RED clause 2's fix that
// live in this package: [journeyEngine.approverForNode] and
// [journeyApprovalOutcome]'s finance/manager derivation, neither of which
// journey_decide_test.go's existing fixtures reach (they all use
// prototype.NodeApproval, the default branch that keeps the base approver
// unchanged).

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestApproverForNodeDerivesDistinctPrincipalsForFinanceAndManager(t *testing.T) {
	engine := newJourneyEngine(nil, nil, "principal:promoux003-base", nil, nil)

	finance, err := engine.approverForNode(promotionexec.NodeApproveFinance)
	if err != nil {
		t.Fatalf("approverForNode(finance): %v", err)
	}
	manager, err := engine.approverForNode(promotionexec.NodeApproveManager)
	if err != nil {
		t.Fatalf("approverForNode(manager): %v", err)
	}
	if finance == manager {
		t.Fatalf("finance %q and manager %q must not be the same principal (RED clause 2)", finance, manager)
	}
	wantFinance, err := promotionexec.FinanceApproverFor(engine.approver)
	if err != nil {
		t.Fatalf("FinanceApproverFor: %v", err)
	}
	wantManager, err := promotionexec.ManagerApproverFor(engine.approver)
	if err != nil {
		t.Fatalf("ManagerApproverFor: %v", err)
	}
	if finance != wantFinance || manager != wantManager {
		t.Fatalf("approverForNode = (%q, %q), want the exact derivation execution.go's factory uses: (%q, %q)",
			finance, manager, wantFinance, wantManager)
	}
	if err := approverclass.RequireDistinct(finance, manager); err != nil {
		t.Fatalf("RequireDistinct(finance, manager) = %v, want nil", err)
	}

	// A node that is neither finance nor manager (the prototype's own
	// approval node, or a plain Task) has no authority class to
	// differentiate and keeps the base approver unchanged.
	other, err := engine.approverForNode("some_other_node")
	if err != nil {
		t.Fatalf("approverForNode(other): %v", err)
	}
	if other != engine.approver {
		t.Fatalf("approverForNode for an undifferentiated node = %q, want the base approver %q", other, engine.approver)
	}
}

// journeyPromotionexecFixture builds a routed, completed APPROVAL WorkItem
// for one of promotionexec's real finance/manager nodes, exactly the shape
// internal/platform/execution's WorkItemFactory and this engine's own
// completeApproval leave behind: the compiled requirement's digests on the
// assignment, the requirement's own deadline, and a candidate set naming the
// class-derived approver (never the bare base).
func journeyPromotionexecFixture(
	t *testing.T, nodeID string,
	compile func(approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error),
	deriveApprover func(base string) (string, error),
	approve bool,
) (workitem.WorkItem, intent.ProposalRevision, intentapproval.ApprovalDecision, time.Time) {
	t.Helper()
	const base = "principal:promoux003-fixture-base"
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	approver, err := deriveApprover(base)
	if err != nil {
		t.Fatalf("derive approver: %v", err)
	}
	requirement, err := compile(approver, at.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("compile requirement: %v", err)
	}
	intentID, revisionID := "intent:promoux003-fixture", "revision-1"
	revision := intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1, AlgorithmID: "sha256",
			Digest: strings.Repeat("a", 64), ScopeBindingDigest: strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
	item := workitem.WorkItem{
		TenantID:               uuid.MustParse("11111111-2222-4333-8444-555555555555"),
		WorkItemID:             uuid.New(),
		WorkflowInstanceID:     uuid.MustParse("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"),
		ItemVersion:            3,
		Kind:                   workitem.KindApproval,
		NodeID:                 nodeID,
		Status:                 workitem.StatusInProgress,
		ApprovalRequirementRef: requirement.RequirementID,
		ProposalRef:            revision.MaterialDigest.Digest,
		DeadlineAt:             requirement.Deadline.Expiry.Time(),
		AssignmentDigest:       "sha256:assignment",
		Assignment: workitem.Assignment{
			Resolution: humanwork.Resolution{
				RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision,
				RequirementDigest: requirement.Digest(), ExpressionDigest: requirement.ExpressionDigest,
				QuorumRequired: requirement.Quorum.MinApprovals,
				Candidates: []humanwork.Candidate{
					{PrincipalID: approver, Via: humanwork.SourceDirect},
				},
			},
		},
	}
	engine := newJourneyEngine(nil, nil, base, nil, nil)
	decision := engine.approvalDecision(item, intent.Instance{IntentID: intentID}, revisionID,
		revision.MaterialDigest, workspace.Decision{Approve: approve, Reason: "reason"}, at, approver)
	item.Status = workitem.StatusCompleted
	item.CompletedBy = decision.Approver.PrincipalID
	item.CompletedOutputDigest = decision.Digest()
	completedAt := at
	item.CompletedAt = &completedAt
	return item, revision, decision, at
}

func TestJourneyApprovalOutcomeDerivesTheFinanceApproverFromTheBase(t *testing.T) {
	item, revision, decision, at := journeyPromotionexecFixture(
		t, promotionexec.NodeApproveFinance, promotionexec.CompileFinanceApprovalRequirement, promotionexec.FinanceApproverFor, true)
	outcome, err := journeyApprovalOutcome(item, "principal:promoux003-fixture-base", revision, decision, at)
	if err != nil {
		t.Fatalf("journeyApprovalOutcome(finance): %v", err)
	}
	if outcome.Outcome != workflow.Outcome("APPROVED") || outcome.Failed {
		t.Fatalf("finance outcome = %+v, want a routable APPROVED", outcome)
	}
}

func TestJourneyApprovalOutcomeDerivesTheManagerApproverFromTheBase(t *testing.T) {
	item, revision, decision, at := journeyPromotionexecFixture(
		t, promotionexec.NodeApproveManager, promotionexec.CompileManagerApprovalRequirement, promotionexec.ManagerApproverFor, false)
	outcome, err := journeyApprovalOutcome(item, "principal:promoux003-fixture-base", revision, decision, at)
	if err != nil {
		t.Fatalf("journeyApprovalOutcome(manager): %v", err)
	}
	if outcome.Outcome != workflow.OutcomeRejected {
		t.Fatalf("manager outcome = %+v, want REJECTED (the compiled edge key)", outcome)
	}
}

// TestJourneyApprovalOutcomeRefusesTheUndifferentiatedBaseForPromotionexecNodes
// proves the derivation is load-bearing, not decorative: rebuilding the
// finance requirement straight from the base approver (skipping
// FinanceApproverFor, as pre-PROMOUX-003 code did) no longer matches the
// candidate the item was actually routed to, so Resolve refuses it.
func TestJourneyApprovalOutcomeRefusesTheUndifferentiatedBaseForPromotionexecNodes(t *testing.T) {
	item, revision, decision, at := journeyPromotionexecFixture(
		t, promotionexec.NodeApproveFinance, promotionexec.CompileFinanceApprovalRequirement, promotionexec.FinanceApproverFor, true)
	// Overwrite the recorded candidate as if some caller had pinned the bare
	// base approver directly (the RED clause 2 defect) instead of the
	// class-derived one.
	item.Assignment.Resolution.Candidates = []humanwork.Candidate{
		{PrincipalID: "principal:promoux003-fixture-base", Via: humanwork.SourceDirect},
	}
	if _, err := journeyApprovalOutcome(item, "principal:promoux003-fixture-base", revision, decision, at); err == nil {
		t.Fatal("an item whose candidate is the undifferentiated base approver must not resolve against the class-derived requirement")
	}
}
