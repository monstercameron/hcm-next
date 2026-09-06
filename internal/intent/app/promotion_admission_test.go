package app

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/trust/stepup"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

func TestPromotionApprovalReevaluatesCurrentManagerAuthority(t *testing.T) {
	result, err := RevalidatePromotionAuthority(runtime.PromotionRevalidation{
		PinnedManagerRef:  "manager:before",
		CurrentManagerRef: "manager:after",
	})
	if runtime.CodeOf(err) != runtime.CodeApprovalBindingStale {
		t.Fatalf("manager change code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeApprovalBindingStale, err)
	}
	if result.Confirmed || result.ChangedFact != "manager_relationship" {
		t.Fatalf("manager change result = %+v, want an unconfirmed manager relationship", result)
	}
	if !strings.Contains(err.Error(), "manager_relationship") || !strings.Contains(err.Error(), runtime.CodeReapprovalRequired) {
		t.Fatalf("manager change refusal = %v, want the changed relationship and reapproval route", err)
	}
}

func TestPromotionExecutionRequiresStepUpOrAdditionalApprovalForSensitiveAccess(t *testing.T) {
	base := PromotionExecutionAdmission{
		ProposalID:           "proposal-1",
		MaterialDigest:       "sha256:" + strings.Repeat("a", 64),
		ExistingAccessScopes: []string{"employee.read"},
		ProposedAccessScopes: []string{"employee.read", PromotionAccessScopeDirectReports, PromotionAccessScopeCompensationDirectReports},
		PrimaryApproval: PromotionAccessApproval{
			PrincipalID: "principal:manager",
			Approved:    true,
		},
	}

	result, err := AdmitPromotionExecution(base)
	if err == nil {
		t.Fatal("sensitive access expansion without a control was admitted")
	}
	if result.SensitiveAccessExpanded != true || len(result.ExpandedScopes) != 2 {
		t.Fatalf("sensitive expansion result = %+v, want both protected scopes", result)
	}
	refusal, ok := err.(*envelope.Error)
	if !ok {
		t.Fatalf("refusal error type = %T, want *envelope.Error", err)
	}
	if refusal.Code() != envelope.CodeFailedPrecondition || refusal.ReasonRef() != PromotionSensitiveAccessCode ||
		!strings.Contains(err.Error(), ReasonPromotionSensitiveAccessStepUpOrApproval) {
		t.Fatalf("refusal = %v, want typed sensitive-access reason", err)
	}

	withSecondApproval := base
	withSecondApproval.AdditionalApprovals = []PromotionAccessApproval{{
		PrincipalID:    "principal:hrbp",
		ProposalID:     base.ProposalID,
		MaterialDigest: base.MaterialDigest,
		RequirementID:  PromotionSensitiveAccessApprovalRequirement,
		Approved:       true,
	}}
	if admitted, err := AdmitPromotionExecution(withSecondApproval); err != nil || admitted.AuthorizedBy != "additional_approval" {
		t.Fatalf("second approval admission = %+v, %v", admitted, err)
	}

	withStepUp := base
	withStepUp.SensitiveAccessStepUp = stepup.Obligation{Code: stepup.CodeStepUpSatisfied, Satisfied: true}
	if admitted, err := AdmitPromotionExecution(withStepUp); err != nil || admitted.AuthorizedBy != "internal/trust/stepup" {
		t.Fatalf("step-up admission = %+v, %v", admitted, err)
	}
}

func TestPromotionExecutionDoesNotRequireSensitiveAccessControlForUnchangedScopes(t *testing.T) {
	result, err := AdmitPromotionExecution(PromotionExecutionAdmission{
		ProposalID:           "proposal-2",
		MaterialDigest:       "sha256:" + strings.Repeat("b", 64),
		ExistingAccessScopes: []string{PromotionAccessScopeDirectReports},
		ProposedAccessScopes: []string{PromotionAccessScopeDirectReports},
	})
	if err != nil {
		t.Fatalf("unchanged sensitive scope: %v", err)
	}
	if result.SensitiveAccessExpanded || result.AuthorizedBy != "not_required" {
		t.Fatalf("unchanged sensitive scope result = %+v", result)
	}
}
