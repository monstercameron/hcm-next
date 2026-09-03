package simulate

import (
	"context"

	"github.com/monstercameron/hcm-next/internal/engines/rules"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// Route keys the promotion reference's raise-threshold DECISION declares.
const (
	RouteWithinThreshold  = "WITHIN_THRESHOLD"
	RouteExceedsThreshold = "EXCEEDS_THRESHOLD"
)

// PromotionThresholdRuleRef is the rule reference the promotion reference
// workflow's DECISION node binds. RulesDecisions answers this reference and
// refuses any other, rather than pretending one decision table can answer an
// arbitrary rule.
const PromotionThresholdRuleRef = "rules.compensation.raise_threshold/v3"

// Input field paths the raise-threshold decision reads.
const (
	FieldRaiseRatio   = "raise_ratio"
	FieldBandPosition = "band_position"
)

// RulesDecisions evaluates a DECISION node through internal/engines/rules.
//
// It is the promotion threshold table and nothing else: the evaluator is named
// in the compiled node, and a rule reference this port does not implement is a
// refusal rather than a default route. A decision engine that answered every
// question would be a decision engine nobody could audit.
type RulesDecisions struct {
	// BudgetAuthority is the compensation-pool budget authority backing the
	// raise. The raise-threshold node's declared inputs do not carry it, so it
	// is stated here as run configuration rather than inferred.
	//
	// The zero value means UNKNOWN, and UNKNOWN blocks: RULE-003's contract is
	// that an unresolved budget authority can never resolve to "no finance
	// approval required", so a caller that has not resolved it gets a blocked
	// simulation instead of an optimistic one.
	BudgetAuthority rules.BudgetAuthority
	// GradeChange reports whether the promotion also changes the worker's
	// grade, not merely their job or title.
	GradeChange bool
}

// Decide implements DecisionPort.
func (d RulesDecisions) Decide(_ context.Context, req DecisionRequest) (DecisionResult, error) {
	if req.Decision.RuleRef != PromotionThresholdRuleRef {
		return DecisionResult{}, refuse(CodeHandlerFailed, req.NodeID,
			"no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	ratio, err := req.Inputs.Get(FieldRaiseRatio)
	if err != nil {
		return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "raise threshold")
	}
	increase, err := ratio.Decimal()
	if err != nil {
		return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "raise threshold")
	}
	position, err := req.Inputs.Text(FieldBandPosition)
	if err != nil {
		return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "raise threshold")
	}
	budget := d.BudgetAuthority
	if budget == rules.BudgetAuthorityUnspecified {
		budget = rules.BudgetAuthorityUnknown
	}
	band := rules.BandPosition(position)
	if !band.Valid() {
		band = rules.BandPositionUnknown
	}

	decision, err := rules.EvaluatePromotionApproval(rules.PromotionApprovalThresholdTable(), rules.PromotionApprovalInput{
		IncreasePercent: increase,
		BandPosition:    band,
		BudgetAuthority: budget,
		GradeChange:     d.GradeChange,
	})
	if err != nil {
		return DecisionResult{}, wrap(CodeHandlerFailed, req.NodeID, err,
			"promotion approval threshold table")
	}

	route := ""
	switch decision.Tier {
	case rules.ApprovalTierStandard:
		route = RouteWithinThreshold
	case rules.ApprovalTierFinanceRequired, rules.ApprovalTierExecutiveRequired:
		route = RouteExceedsThreshold
	default:
		// UNKNOWN_BLOCKED is never quietly folded into "within threshold".
		route = string(workflow.OutcomeUnknown)
	}

	traceRef := decision.TableID + "@" + decision.TableVersion + "#" + decision.MatchedRowID
	return DecisionResult{
		RouteKey: route,
		TraceRef: traceRef,
		Detail: "tier " + string(decision.Tier) + " from " + decision.TableID +
			"@" + decision.TableVersion + " row " + decision.MatchedRowID +
			" (increase " + increase.String() + "%, band " + string(band) +
			", budget " + string(budget) + ")",
	}, nil
}

// Tier re-evaluates the promotion threshold table for the same inputs a run's
// DECISION node used. The approval port needs the tier to derive the
// requirement set, and re-running the table is deliberate: deriving approvals
// from a remembered answer would let the two disagree after a table change.
func (d RulesDecisions) Tier(increaseText, bandPosition string) (rules.PromotionApprovalDecision, error) {
	increase, err := increaseDecimal(increaseText)
	if err != nil {
		return rules.PromotionApprovalDecision{}, err
	}
	budget := d.BudgetAuthority
	if budget == rules.BudgetAuthorityUnspecified {
		budget = rules.BudgetAuthorityUnknown
	}
	band := rules.BandPosition(bandPosition)
	if !band.Valid() {
		band = rules.BandPositionUnknown
	}
	return rules.EvaluatePromotionApproval(rules.PromotionApprovalThresholdTable(), rules.PromotionApprovalInput{
		IncreasePercent: increase,
		BandPosition:    band,
		BudgetAuthority: budget,
		GradeChange:     d.GradeChange,
	})
}

// increaseDecimal parses a raise percentage at the scale the threshold table
// declares. A caller's decimal may be stated at any scale; the comparison is
// numeric, so a percentage measured to whole points still compares exactly
// against a four-digit threshold.
func increaseDecimal(text string) (values.Decimal, error) {
	return values.NewDecimal(text, rules.IncreasePercentScale, values.RoundingHalfEven)
}
