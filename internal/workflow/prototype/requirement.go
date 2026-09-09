package prototype

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The compiled human-work requirement behind [ApprovalDefinition]'s one
// APPROVAL node.
//
// [ApprovalDefinition] declares the requirement by id only; the workflow
// compiler never sees candidate resolution, deadlines or provenance, because
// those are internal/humanwork's business (APPROVAL-001..003). But the two
// packages that act on the parked node both need the compiled form: the
// work-item factory that routes the approval has to record the requirement's
// digest on the assignment, and the caller that resumes the node has to
// present that same digest when internal/workflow/steps/approval.Resolve
// re-checks the completed item against it. This file is the one place both
// get it from, so they cannot disagree.

// Fixed identifiers the prototype requirement cites. They are constants
// rather than configuration because the requirement's digest covers them: a
// composition that changed one would route work items no later Resolve could
// bind.
const (
	// ApprovalPinnedPolicyRef is the policy a NAMED candidate expression must
	// pin its approver under (humanwork.Expression's own rule: a pinned person
	// with no policy behind it is refused).
	ApprovalPinnedPolicyRef = "policy.prototype.promotion-approver/v1"
	// ApprovalGovernancePolicyRef is the governance configuration the
	// requirement's provenance cites, and the reference the routed
	// assignment carries as its GovernancePolicyRef.
	ApprovalGovernancePolicyRef = "governance.execution-authority/1"
	// ApprovalAuthorityFloor is the one role a decision on the prototype
	// requirement needs.
	ApprovalAuthorityFloor = "promotion_approver"
)

// CompileApprovalRequirement compiles the prototype's approval requirement for
// one routed approver and one decision deadline.
//
// The requirement is a function of exactly those two inputs, and its digest
// therefore is too. That is deliberate: the caller that resumes the node holds
// the routed WorkItem, whose DeadlineAt is this deadline and whose assignment
// names this approver, so it can rebuild the identical requirement from
// durable rows alone and satisfy Resolve's binding check without any second
// record of "which requirement was this".
//
// decideBy is truncated to the second before it is bound. A PostgreSQL
// timestamptz keeps microseconds and a Go instant keeps nanoseconds; a
// deadline that did not survive that round trip byte for byte would rebuild
// to a different digest than the one the assignment recorded, and every
// decision on the item would be refused as a binding mismatch for a reason
// nobody could see. A whole-second deadline survives the trip exactly.
// Expiry equals DecideBy: the prototype has no separate decision-freshness
// window, and a decision recorded in time is current until the node is
// resumed.
func CompileApprovalRequirement(approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	if approver == "" {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("prototype: the approval requirement needs an approver principal")
	}
	if decideBy.IsZero() {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("prototype: the approval requirement needs a decision deadline")
	}
	deadline := values.NewInstant(decideBy.UTC().Truncate(time.Second))
	return humanwork.Compile(humanwork.RequirementSpec{
		RequirementID: ApprovalRequirementID,
		Revision:      1,
		Stage:         1,
		Candidates: humanwork.Named(approver, ApprovalPinnedPolicyRef,
			humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: "prototype/people"}),
		AuthorityFloor: []string{ApprovalAuthorityFloor},
		Quorum:         humanwork.Quorum{MinApprovals: 1},
		Deadline:       humanwork.Deadline{DecideBy: deadline, Expiry: deadline},
		Escalation: humanwork.EscalationPolicy{
			OnDeadline: humanwork.EscalationBlock,
			RuleID:     "rule.prototype.escalation.block/v1",
		},
		Separation: humanwork.SeparationConstraints{
			RequesterMayNotApprove: true,
			SubjectMayNotApprove:   true,
			RuleID:                 "rule.prototype.separation/v1",
		},
		Invalidators: []humanwork.Invalidator{{
			Kind:   humanwork.InvalidatorMaterialProposalChange,
			RuleID: "rule.prototype.invalidate.material_change/v1",
		}},
		Source: humanwork.RequirementSource{
			Tier:                rules.ApprovalTierStandard,
			TableID:             "prototype.promotion.approval_tier",
			TableVersion:        "1",
			TableDigest:         "sha256:prototype-approval-tier",
			MatchedRowID:        "prototype.standard",
			GovernancePolicyRef: ApprovalGovernancePolicyRef,
		},
	})
}
