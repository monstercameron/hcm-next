package promotionexec

import (
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/rules"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	FinanceApprovalPinnedPolicyRef = "policy.promotion.finance-partner/v1"
	ManagerApprovalPinnedPolicyRef = "policy.promotion.current-manager/v1"
	FinanceApprovalGovernanceRef   = "governance.promotion.finance-partner/v1"
	ManagerApprovalGovernanceRef   = "governance.promotion.current-manager/v1"
	FinanceApprovalAuthorityFloor  = "finance_partner"
	ManagerApprovalAuthorityFloor  = "current_manager"
)

func compileApprovalRequirement(id, approver string, decideBy time.Time, policyRef, governanceRef, authorityFloor string) (humanwork.ApprovalRequirement, error) {
	if approver == "" {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("promotionexec: the approval requirement needs an approver principal")
	}
	if decideBy.IsZero() {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("promotionexec: the approval requirement needs a decision deadline")
	}
	deadline := values.NewInstant(decideBy.UTC().Truncate(time.Second))
	return humanwork.Compile(humanwork.RequirementSpec{
		RequirementID: id,
		Revision:      1,
		Stage:         1,
		Candidates: humanwork.Named(approver, policyRef,
			humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: organizationScope}),
		AuthorityFloor: []string{authorityFloor},
		Quorum:         humanwork.Quorum{MinApprovals: 1},
		Deadline:       humanwork.Deadline{DecideBy: deadline, Expiry: deadline},
		Escalation: humanwork.EscalationPolicy{
			OnDeadline: humanwork.EscalationBlock,
			RuleID:     "rule.promotion.escalation.block/v1",
		},
		Separation: humanwork.SeparationConstraints{
			RequesterMayNotApprove: true,
			SubjectMayNotApprove:   true,
			RuleID:                 "rule.promotion.separation/v1",
		},
		Invalidators: []humanwork.Invalidator{{
			Kind:   humanwork.InvalidatorMaterialProposalChange,
			RuleID: "rule.promotion.invalidate.material_change/v1",
		}},
		Source: humanwork.RequirementSource{
			Tier:                rules.ApprovalTierStandard,
			TableID:             "promotion.approval.tier",
			TableVersion:        "1",
			TableDigest:         "sha256:promotion-approval-tier",
			MatchedRowID:        "promotion.standard",
			GovernancePolicyRef: governanceRef,
		},
	})
}

// CompileFinanceApprovalRequirement mirrors prototype.CompileApprovalRequirement
// for the Finance Partner approval.
func CompileFinanceApprovalRequirement(approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	return compileApprovalRequirement(ApprovalFinance, approver, decideBy, FinanceApprovalPinnedPolicyRef, FinanceApprovalGovernanceRef, FinanceApprovalAuthorityFloor)
}

// CompileManagerApprovalRequirement mirrors prototype.CompileApprovalRequirement
// for the current-manager approval.
func CompileManagerApprovalRequirement(approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	return compileApprovalRequirement(ApprovalManager, approver, decideBy, ManagerApprovalPinnedPolicyRef, ManagerApprovalGovernanceRef, ManagerApprovalAuthorityFloor)
}
