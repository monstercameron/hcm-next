package productui

// PROMOUX-007: "Present promotion validation as field-linked recoverable
// guidance."
//
// RED: a rejected pay value produces a page-level message containing field
// keys, decimal fractions, Go error names or correlation internals, does not
// associate the error with Proposed base pay, or leaves the user at an
// unrelated scroll position.
//
// GREEN: typed refusal reasons map to localized field messages with exact
// corrective bounds; the summary links to each invalid field; focus and
// scroll move predictably without losing entered values; changing a field
// clears only its stale error; a copyable support reference is available
// through diagnostics without implementation text in ordinary copy.
//
// REFACTOR: one refusal-to-presentation mapper serves SSR and enhanced
// clients; do not parse human error strings.
//
// This file is the mapper REFACTOR requires: [MapPromotionRefusal] turns an
// already-computed [promotion.PreflightResult] into the [ValidationState]
// WEB-125's ValidationSummary/ValidationInput already know how to render and
// associate accessibly, plus a diagnostics-only [PromotionValidationDiagnostics].
// It is the ONLY place a promotion.Finding.Code becomes UI copy, and it is
// never called twice with two different implementations: render.go's
// server-rendered path (ui.RenderToString(Build(view))) and mount_wasm.go's
// enhanced-client path (ui.Render(Build(view), selector)) both walk the exact
// same Go component tree built by Build(view), because GoWebComponents
// compiles this package once for native SSR and once for js/wasm. There is
// no second, JS-side reimplementation of this mapper for either build to
// drift from.
//
// Every branch below switches on Finding.Code -- the typed identity the
// promotion domain already emits -- never on Finding.Message, which the
// domain documents as an operator-facing sentence that may carry raw
// decimal fractions (e.g. compa-ratio), internal reasons ("authorization
// denied this field: ...") or Go error text (ParseLocalDate failures). This
// file does not read Finding.Message at all, so none of that can reach
// ordinary copy no matter what a future rule writes into it.
//
// Exact corrective bounds (GREEN) come from the already-evaluated
// [promotion.CompensationGuardrail] PROMOUX-006 computed -- this file
// performs no money or percentage arithmetic and never re-derives a range
// from a percentage; see promotionBandBoundMessage.
import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PromotionValidationDiagnostics is GREEN's "copyable support reference...
// available through diagnostics" -- reachable only through
// [PromotionValidationDiagnosticsPanel], never mixed into the
// [ValidationState] ordinary copy renders from. SupportReference is the
// domain's own [promotion.PreflightResult.ResultDigest]: an opaque,
// reproducible hash a support agent can be given to locate the exact
// evaluation, never a raw correlation id, trace reference or Go error text.
type PromotionValidationDiagnostics struct {
	Available        bool
	SupportReference string
}

// MapPromotionRefusal is REFACTOR's one refusal-to-presentation mapper. It
// maps every finding on an already-evaluated [promotion.PreflightResult]
// into a field-linked [ValidationState], and separately resolves GREEN's
// diagnostics-only support reference. Diagnostics is only Available when the
// result actually blocks (there is something worth a support agent
// investigating) and the domain supplied a non-empty digest to cite; a
// clean or advisory-only result never manufactures a placeholder reference.
func MapPromotionRefusal(locale LocaleContext, result promotion.PreflightResult, guardrail promotion.CompensationGuardrail) (ValidationState, PromotionValidationDiagnostics) {
	issues := make([]ValidationIssue, 0, len(result.Findings))
	for _, finding := range result.Findings {
		issues = append(issues, promotionValidationIssueFrom(locale, finding, guardrail))
	}
	diagnostics := PromotionValidationDiagnostics{}
	if digest := strings.TrimSpace(result.ResultDigest); digest != "" && len(result.Blocking()) > 0 {
		diagnostics = PromotionValidationDiagnostics{Available: true, SupportReference: digest}
	}
	return ValidationState{Issues: issues}, diagnostics
}

// promotionValidationIssueFrom projects one typed finding into one
// presentation issue. FieldID is the domain's own Finding.Field passed
// through unchanged: it is already a stable, typed identity (not parsed
// from a message), and using it directly as the rendered control's id is
// what lets [ValidationSummary] link to the real field without a second
// translation table that could drift from the actual form.
func promotionValidationIssueFrom(locale LocaleContext, finding promotion.Finding, guardrail promotion.CompensationGuardrail) ValidationIssue {
	return ValidationIssue{
		FieldID:  strings.TrimSpace(finding.Field),
		Code:     finding.Code,
		Message:  promotionFindingMessage(locale, finding, guardrail),
		Severity: promotionFindingValidationSeverity(finding.Severity),
	}
}

// promotionFindingValidationSeverity fails closed to Error: only the
// domain's own SeverityAdvisory ever downgrades to a warning. An
// unspecified or future severity this file predates renders as an
// actionable error rather than silently becoming invisible.
func promotionFindingValidationSeverity(severity promotion.Severity) ValidationSeverity {
	if severity == promotion.SeverityAdvisory {
		return ValidationSeverityWarning
	}
	return ValidationSeverityError
}

// promotionFindingMessage is the exhaustive Code -> copy switch clause 4 of
// PROMOUX-007 requires: every code the promotion domain can emit today has
// its own explicit case, and the switch has no case that returns another
// code's text. A code this file does not recognise -- one a future rule
// adds after this file was last updated -- falls through to the single
// generic "unrecognized_reason" key, never to Finding.Message and never to
// Finding.Code itself, so an unrecognised refusal fails closed to safe copy
// instead of leaking the raw identifier.
func promotionFindingMessage(locale LocaleContext, finding promotion.Finding, guardrail promotion.CompensationGuardrail) string {
	switch finding.Code {
	case promotion.CodeCurrentAmountInvalid:
		return locale.Text("promotion_validation.current_amount_invalid")
	case promotion.CodeProposedAmountInvalid:
		return locale.Text("promotion_validation.proposed_amount_invalid")
	case promotion.CodeCurrencyRequired:
		return locale.Text("promotion_validation.currency_required")
	case promotion.CodePayBasisRequired:
		return locale.Text("promotion_validation.pay_basis_required")
	case promotion.CodeCurrencyChangeNotV0:
		return locale.Text("promotion_validation.currency_change_not_supported")
	case promotion.CodeNotARaise:
		return locale.Text("promotion_validation.not_a_raise")
	case promotion.CodeBusinessReasonRequired:
		return locale.Text("promotion_validation.business_reason_required")
	case promotion.CodeEffectiveAtRequired:
		return locale.Text("promotion_validation.effective_date_required")
	case promotion.CodeEffectiveAtTooFarPast:
		return locale.Text("promotion_validation.effective_date_too_far_past")
	case promotion.CodeIncreaseOverThreshold:
		return locale.Text("promotion_validation.increase_over_threshold")
	case promotion.CodeSubjectNotDisclosable:
		return locale.Text("promotion_validation.subject_not_disclosable")
	case promotion.CodeRequiredFieldDenied:
		return locale.Text("promotion_validation.required_field_denied")
	case promotion.CodeRequiredFieldUnavailabl:
		return locale.Text("promotion_validation.required_field_unavailable")
	case promotion.CodeWorkerNotActive:
		return locale.Text("promotion_validation.worker_not_active")
	case promotion.CodeTargetJobRequired:
		return locale.Text("promotion_validation.target_job_required")
	case promotion.CodeTargetGradeRequired:
		return locale.Text("promotion_validation.target_grade_required")
	case promotion.CodeSameGrade:
		return locale.Text("promotion_validation.same_grade")
	case promotion.CodeEffectiveBeforeHire:
		return locale.Text("promotion_validation.effective_before_hire")
	case promotion.CodePayBandNotFound:
		return locale.Text("promotion_validation.pay_band_not_found")
	case promotion.CodeBelowBandMinimum:
		return promotionBandBoundMessage(locale, "promotion_validation.below_band_minimum",
			"promotion_validation.below_band_minimum_generic", guardrail, guardrail.MinimumAnnualized)
	case promotion.CodeAboveBandMaximum:
		return promotionBandBoundMessage(locale, "promotion_validation.above_band_maximum",
			"promotion_validation.above_band_maximum_generic", guardrail, guardrail.MaximumAnnualized)
	case promotion.CodeBudgetAuthorityMissing:
		return locale.Text("promotion_validation.budget_authority_missing")
	case promotion.CodeBudgetObservationOnly:
		return locale.Text("promotion_validation.budget_observation_only")
	case promotion.CodeBudgetObservedShort:
		return locale.Text("promotion_validation.budget_observed_short")
	case promotion.CodeTargetManagerNotFound:
		return locale.Text("promotion_validation.target_manager_not_found")
	case promotion.CodeManagerRelationshipCycle:
		return locale.Text("promotion_validation.manager_relationship_cycle")
	case promotion.CodeManagerChainUnresolved:
		return locale.Text("promotion_validation.manager_chain_unresolved")
	case promotion.CodeTargetPositionNotFound:
		return locale.Text("promotion_validation.target_position_not_found")
	case promotion.CodeTargetPositionIncompatible:
		return locale.Text("promotion_validation.target_position_incompatible")
	case promotion.CodeTargetPositionNotEffective:
		return locale.Text("promotion_validation.target_position_not_effective")
	case promotion.CodeTargetPositionAtCapacity:
		return locale.Text("promotion_validation.target_position_at_capacity")
	case promotion.CodeTargetPositionReservationConflict:
		return locale.Text("promotion_validation.target_position_reservation_conflict")
	default:
		return locale.Text("promotion_validation.unrecognized_reason")
	}
}

// promotionBandBoundMessage quotes GREEN's exact corrective bound -- the
// guardrail's own already-computed MinimumAnnualized or MaximumAnnualized,
// formatted through the shared [money] helper -- and never computes one
// itself. When the guardrail is not available (unauthorized, or the target
// band could not be resolved) or the requested bound itself failed to
// validate, quoting a number would either disclose nothing trustworthy or
// risk showing a zero value as if it were a real bound, so this falls
// closed to the generic key with no amount at all.
func promotionBandBoundMessage(locale LocaleContext, boundedKey, genericKey string, guardrail promotion.CompensationGuardrail, bound values.Money) string {
	if !guardrail.Available() || bound.Validate() != nil {
		return locale.Text(genericKey)
	}
	return locale.Text(boundedKey, map[string]string{"amount": money(locale, bound)})
}

// ClearFieldIssue is GREEN's precision requirement: "changing a field
// clears only its stale error." It returns a copy of state with fieldID's
// own issue removed and every other field's issue left byte-for-byte
// untouched. The easy wrong implementation clears the whole ValidationState
// on any edit, which would also discard a still-invalid field's error the
// moment an unrelated field changes -- see TestTodo_PROMOUX_007's
// two-invalid-fields case, which is exactly the scenario a single-field test
// cannot distinguish this from.
func ClearFieldIssue(state ValidationState, fieldID string) ValidationState {
	fieldID = strings.TrimSpace(fieldID)
	if fieldID == "" || len(state.Issues) == 0 {
		return state
	}
	kept := make([]ValidationIssue, 0, len(state.Issues))
	for _, issue := range state.Issues {
		if strings.TrimSpace(issue.FieldID) == fieldID {
			continue
		}
		kept = append(kept, issue)
	}
	if len(kept) == len(state.Issues) {
		return state
	}
	return ValidationState{Issues: kept, SubmissionAttempted: state.SubmissionAttempted}
}

// PromotionValidationDiagnosticsPanel renders GREEN's "copyable support
// reference... available through diagnostics" as a collapsed disclosure,
// structurally separate from ValidationSummary and ValidationInput: its
// content is never mixed into the field-linked error text those components
// render, so ordinary copy stays exactly as clean as
// TestTodo_PROMOUX_007_Security requires while the reference stays
// reachable one click away. When Diagnostics.Available is false -- no
// blocking finding, or the domain result carried no digest -- nothing
// renders at all, because a blank or placeholder support reference is not a
// real one.
//
// The reference is presented as a read-only, selectable text field rather
// than a scripted clipboard action: it is copyable by the same native
// select-and-copy gesture as any other on-screen value, with no clipboard
// permission or JS dependency required to work.
func PromotionValidationDiagnosticsPanel(locale LocaleContext, diagnostics PromotionValidationDiagnostics) ui.Node {
	if !diagnostics.Available {
		return html.Div(html.Props{Hidden: true, Class: "promotion-validation-diagnostics-empty"})
	}
	i18n := I18nProps{Locale: locale}
	return html.Details(html.Props{Class: "promotion-validation-diagnostics", Dir: string(locale.Direction)},
		html.Summary(html.Props{}, ui.Text(i18n.Text("promotion_validation.diagnostics_disclosure"))),
		html.Label(html.Props{Class: "promotion-validation-diagnostics-label"},
			ui.Text(i18n.Text("promotion_validation.support_reference_label")),
			html.Input(html.Props{
				Type:     "text",
				ReadOnly: true,
				Value:    diagnostics.SupportReference,
				Class:    "promotion-validation-support-reference",
			}),
		),
	)
}
