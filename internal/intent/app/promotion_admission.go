package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Stable reason references for the two promotion admission boundaries owned
// by this application cell.
const (
	ReasonPromotionSensitiveAccessStepUpOrApproval = "promotion.sensitive_access.step_up_or_additional_approval_required"
	PromotionSensitiveAccessCode                   = ReasonPromotionSensitiveAccessStepUpOrApproval
	PromotionSensitiveAccessApprovalRequirement    = "approval.promotion.sensitive_access/v1"
	PromotionAccessScopeDirectReports              = "direct_reports"
	PromotionAccessScopeCompensationDirectReports  = "compensation.direct_reports.read"
)

// ErrPromotionSensitiveAccess is the local matchable cause for a promotion
// that would expand sensitive management access without the required control.
var ErrPromotionSensitiveAccess = errors.New("app: promotion sensitive access requires step-up or an additional approval")

// PromotionAccessApproval is the server-held evidence for one approval. The
// proposal identity is repeated here because an approval for another proposal
// must never satisfy this execution boundary.
type PromotionAccessApproval struct {
	PrincipalID    string
	ProposalID     string
	MaterialDigest string
	RequirementID  string
	Approved       bool
}

// PromotionExecutionAdmission is the exact control input evaluated before the
// promotion mutation. Existing and proposed scopes are facts from the
// governed access calculation; callers cannot use this type to assert that a
// scope is harmless because sensitive additions are detected by this package.
type PromotionExecutionAdmission struct {
	ProposalID            string
	MaterialDigest        string
	ExistingAccessScopes  []string
	ProposedAccessScopes  []string
	PrimaryApproval       PromotionAccessApproval
	AdditionalApprovals   []PromotionAccessApproval
	SensitiveAccessStepUp stepup.Obligation
}

// PromotionExecutionAdmissionResult is the explainable result of the pure
// pre-execution gate. It carries the exact sensitive additions and the control
// that admitted them without exposing a proof or other credential material.
type PromotionExecutionAdmissionResult struct {
	SensitiveAccessExpanded bool
	ExpandedScopes          []string
	AuthorizedBy            string
}

// RevalidatePromotionAuthority delegates the current-versus-pinned workforce
// fact check to the workflow execution boundary. It intentionally returns the
// runtime's typed error unchanged, so a changed manager relationship remains
// visible as APPROVAL_BINDING_STALE with manager_relationship in the refusal.
func RevalidatePromotionAuthority(binding runtime.PromotionRevalidation) (runtime.PromotionRevalidationResult, error) {
	return runtime.EvaluatePromotionRevalidation(binding)
}

// AdmitPromotionExecution enforces the access-expansion control immediately
// before execute_promotion. A management promotion that adds either protected
// direct-report scope or compensation.direct_reports.read needs a satisfied
// internal/trust/stepup obligation or one distinct, proposal-bound additional
// approval. This function is pure: it cannot execute or consume anything.
func AdmitPromotionExecution(in PromotionExecutionAdmission) (PromotionExecutionAdmissionResult, error) {
	if strings.TrimSpace(in.ProposalID) == "" || strings.TrimSpace(in.MaterialDigest) == "" {
		return PromotionExecutionAdmissionResult{}, envelope.New(envelope.CodeInvalidArgument,
			"promotion.execution.invalid_binding", "the promotion execution binding is incomplete")
	}

	existing := scopeSet(in.ExistingAccessScopes)
	proposed := scopeSet(in.ProposedAccessScopes)
	added := make([]string, 0)
	for scope := range proposed {
		if _, ok := existing[scope]; !ok {
			added = append(added, scope)
		}
	}
	sort.Strings(added)
	sensitive := filterSensitivePromotionScopes(added)
	result := PromotionExecutionAdmissionResult{
		SensitiveAccessExpanded: len(sensitive) > 0,
		ExpandedScopes:          sensitive,
	}
	if len(sensitive) == 0 {
		result.AuthorizedBy = "not_required"
		return result, nil
	}

	if in.SensitiveAccessStepUp.Code == stepup.CodeStepUpSatisfied && in.SensitiveAccessStepUp.Satisfied {
		result.AuthorizedBy = "internal/trust/stepup"
		return result, nil
	}
	if hasAdditionalPromotionApproval(in) {
		result.AuthorizedBy = "additional_approval"
		return result, nil
	}

	return result, envelope.New(envelope.CodeFailedPrecondition,
		ReasonPromotionSensitiveAccessStepUpOrApproval,
		"sensitive management access expansion requires step-up or an additional approval").
		WithViolation("proposed_access_scopes",
			fmt.Sprintf("the promotion adds protected access scopes: %s", strings.Join(sensitive, ", ")),
			ReasonPromotionSensitiveAccessStepUpOrApproval).
		WithDiagnostic(ErrPromotionSensitiveAccess)
}

func scopeSet(scopes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			set[scope] = struct{}{}
		}
	}
	return set
}

func filterSensitivePromotionScopes(scopes []string) []string {
	protected := map[string]struct{}{
		PromotionAccessScopeDirectReports:             {},
		PromotionAccessScopeCompensationDirectReports: {},
	}
	result := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if _, ok := protected[scope]; ok {
			result = append(result, scope)
		}
	}
	return result
}

func hasAdditionalPromotionApproval(in PromotionExecutionAdmission) bool {
	if strings.TrimSpace(in.PrimaryApproval.PrincipalID) == "" || !in.PrimaryApproval.Approved {
		return false
	}
	for _, approval := range in.AdditionalApprovals {
		if !approval.Approved || strings.TrimSpace(approval.PrincipalID) == "" ||
			approval.PrincipalID == in.PrimaryApproval.PrincipalID ||
			approval.ProposalID != in.ProposalID ||
			approval.MaterialDigest != in.MaterialDigest ||
			approval.RequirementID != PromotionSensitiveAccessApprovalRequirement {
			continue
		}
		return true
	}
	return false
}
