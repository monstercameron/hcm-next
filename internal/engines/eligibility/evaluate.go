// ELIG-003: evaluate eligibility deterministically. ELIG-004: preserve
// conditional, unknown and partial semantics rather than defaulting an
// undetermined branch to a boolean.
//
// Evaluate is a pure function of (facts, rules, request, plan): it performs
// no write of any kind, injects no override, and returns a value. Human
// discretion can only ever enter as a RULE leaf whose RuleReader
// implementation happens to be backed by a recorded human decision; there is
// no separate override parameter this function accepts that could silently
// convert a computed status.
package eligibility

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// leafState is the four-valued evaluation lattice for one leaf or composite:
// PASS, FAIL, PARTIAL (conditional) and UNKNOWN (no determinable evidence).
// leafNotApplicable is a fifth bookkeeping value that composite folding skips
// entirely rather than treating as any of the four.
type leafState uint8

const (
	leafPass leafState = iota
	leafFail
	leafPartial
	leafUnknown
	leafNotApplicable
)

// Evaluate answers req using plan's compiled criteria, reading facts through
// facts and rule outcomes through rules. Identical facts, rules, request and
// plan always produce a byte-identical Result.Digest.
func Evaluate(ctx context.Context, facts FactReader, rules RuleReader, req Request, plan CompiledPlan) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, err
	}

	trace := &evalTrace{}
	state, err := evaluateCondition(ctx, facts, rules, req, plan, plan.Criteria.Root, trace)
	if err != nil {
		return Result{}, err
	}

	status := statusFor(state)
	result := Result{
		Status:            status,
		ProgramVersion:    req.SubjectMatter.Revision,
		FactSnapshotRef:   req.Snapshots.FactSnapshotRef,
		RuleSnapshotRef:   req.Snapshots.RuleSnapshotRef,
		EffectiveInterval: req.EffectiveInterval,
		Reasons:           trace.reasons,
		Obligations:       trace.obligations,
		Evidence:          trace.evidence,
		MissingFacts:      trace.missing,
	}
	if len(result.Reasons) == 0 {
		result.Reasons = []Reason{{Kind: ReasonNotApplicable, Ref: "criteria"}}
	}
	if len(result.Evidence) == 0 {
		result.Evidence = []string{"criteria:no-reads"}
	}
	if result.Status == StatusUnknown && len(result.MissingFacts) == 0 {
		result.MissingFacts = []string{"criteria:no-determinable-evidence"}
	}
	raw := result.Canonical()
	if raw == nil {
		return Result{}, fmt.Errorf("eligibility: evaluate produced an invalid result: %w", result.Validate())
	}
	result.Digest = canonicalbytes.Digest(raw)
	return result, nil
}

func statusFor(s leafState) Status {
	switch s {
	case leafPass, leafNotApplicable:
		return StatusEligible
	case leafFail:
		return StatusIneligible
	case leafPartial:
		return StatusConditional
	default:
		return StatusUnknown
	}
}

// evalTrace accumulates the safe explanation, obligations, evidence and
// missing-fact references produced while folding a criteria tree.
type evalTrace struct {
	reasons     []Reason
	obligations []Obligation
	evidence    []string
	missing     []string
}

func evaluateCondition(ctx context.Context, facts FactReader, rules RuleReader, req Request, plan CompiledPlan, c Condition, trace *evalTrace) (leafState, error) {
	switch {
	case c.Kind == ConditionAnd:
		return foldAnd(ctx, facts, rules, req, plan, c.Children, trace)
	case c.Kind == ConditionOr:
		return foldOr(ctx, facts, rules, req, plan, c.Children, trace)
	case c.Kind == ConditionNot:
		inner, err := evaluateCondition(ctx, facts, rules, req, plan, c.Children[0], trace)
		if err != nil {
			return leafUnknown, err
		}
		switch inner {
		case leafPass:
			return leafFail, nil
		case leafFail:
			return leafPass, nil
		default:
			return inner, nil
		}
	case c.Kind.isFactLeaf():
		return evaluateFactLeaf(ctx, facts, req, plan, c, trace)
	default: // ConditionRule
		return evaluateRuleLeaf(ctx, rules, req, c, trace)
	}
}

func foldAnd(ctx context.Context, facts FactReader, rules RuleReader, req Request, plan CompiledPlan, children []Condition, trace *evalTrace) (leafState, error) {
	result := leafNotApplicable
	for _, child := range children {
		s, err := evaluateCondition(ctx, facts, rules, req, plan, child, trace)
		if err != nil {
			return leafUnknown, err
		}
		if s == leafNotApplicable {
			continue
		}
		switch {
		case s == leafFail:
			result = leafFail
		case result == leafFail:
			// a fail already recorded; a later branch cannot undo it
		case s == leafUnknown:
			result = leafUnknown
		case s == leafPartial:
			if result != leafUnknown {
				result = leafPartial
			}
		case s == leafPass:
			if result == leafNotApplicable {
				result = leafPass
			}
		}
	}
	return result, nil
}

func foldOr(ctx context.Context, facts FactReader, rules RuleReader, req Request, plan CompiledPlan, children []Condition, trace *evalTrace) (leafState, error) {
	result := leafNotApplicable
	for _, child := range children {
		s, err := evaluateCondition(ctx, facts, rules, req, plan, child, trace)
		if err != nil {
			return leafUnknown, err
		}
		if s == leafNotApplicable {
			continue
		}
		switch {
		case s == leafPass:
			result = leafPass
		case result == leafPass:
			// a passing branch already found; nothing beats it
		case s == leafPartial:
			result = leafPartial
		case s == leafUnknown:
			if result != leafPartial {
				result = leafUnknown
			}
		case s == leafFail:
			if result == leafNotApplicable {
				result = leafFail
			}
		}
	}
	return result, nil
}

func evaluateFactLeaf(ctx context.Context, facts FactReader, req Request, plan CompiledPlan, c Condition, trace *evalTrace) (leafState, error) {
	trace.evidence = append(trace.evidence, "fact:"+c.Field)
	fact, err := facts.ReadFact(ctx, req.Subject, c.Field, req.EffectiveInterval)
	if err != nil {
		trace.missing = append(trace.missing, c.Field)
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonFactUnknown, Ref: c.Field})
		return leafUnknown, nil
	}
	if err := fact.Presence.Validate(); err != nil {
		return leafUnknown, fmt.Errorf("%w: fact %q: %v", ErrPortFailed, c.Field, err)
	}
	switch fact.Presence.State() {
	case values.PresenceValue:
		text, _ := fact.Presence.Get()
		fieldType := plan.fieldTypeOf(c.Field)
		var match bool
		if c.Kind == ConditionGreaterThan || c.Kind == ConditionLessThan {
			match, err = compareOrdered(c.Kind, fieldType, text, c.Value)
		} else {
			match, err = compareEqual(fieldType, text, c.Value)
			if c.Kind == ConditionNotEquals {
				match = !match
			}
		}
		if err != nil {
			return leafUnknown, err
		}
		if match {
			trace.reasons = append(trace.reasons, Reason{Kind: ReasonFactPassed, Ref: c.Field})
			return leafPass, nil
		}
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonFactFailed, Ref: c.Field})
		return leafFail, nil
	case values.PresenceRedacted:
		trace.missing = append(trace.missing, c.Field)
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonFactDenied, Ref: c.Field})
		return leafUnknown, nil
	case values.PresenceNotApplicable:
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonNotApplicable, Ref: c.Field})
		return leafNotApplicable, nil
	default: // ABSENT, NULL, UNKNOWN, UNAVAILABLE
		trace.missing = append(trace.missing, c.Field)
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonFactUnknown, Ref: c.Field})
		return leafUnknown, nil
	}
}

func evaluateRuleLeaf(ctx context.Context, rules RuleReader, req Request, c Condition, trace *evalTrace) (leafState, error) {
	ref := c.RuleID + "@" + c.RuleVersion
	trace.evidence = append(trace.evidence, "rule:"+ref)
	evaluation, err := rules.EvaluateRule(ctx, req.Subject, c.RuleID, c.RuleVersion, req.EffectiveInterval)
	if err != nil {
		trace.missing = append(trace.missing, ref)
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonRuleUnknown, Ref: c.RuleID})
		return leafUnknown, nil
	}
	if !evaluation.Outcome.Valid() {
		return leafUnknown, fmt.Errorf("%w: rule %q returned an invalid outcome", ErrPortFailed, c.RuleID)
	}
	switch evaluation.Outcome {
	case RuleOutcomePass:
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonRulePassed, Ref: c.RuleID})
		return leafPass, nil
	case RuleOutcomeFail:
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonRuleFailed, Ref: c.RuleID})
		return leafFail, nil
	case RuleOutcomePartial:
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonRulePartial, Ref: c.RuleID})
		trace.obligations = append(trace.obligations, Obligation{Reason: ObligationRulePartial, Ref: c.RuleID})
		return leafPartial, nil
	case RuleOutcomeDenied:
		trace.missing = append(trace.missing, ref)
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonRuleDenied, Ref: c.RuleID})
		return leafUnknown, nil
	case RuleOutcomeNotApplicable:
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonNotApplicable, Ref: c.RuleID})
		return leafNotApplicable, nil
	default: // RuleOutcomeUnknown
		trace.missing = append(trace.missing, ref)
		trace.reasons = append(trace.reasons, Reason{Kind: ReasonRuleUnknown, Ref: c.RuleID})
		return leafUnknown, nil
	}
}

// fieldTypeOf returns the declared type Compile recorded for field, or
// FieldTypeString when the field is somehow absent from the compiled plan
// (which Compile's own validation makes unreachable for any leaf it accepted).
func (p CompiledPlan) fieldTypeOf(field string) FieldType {
	if desc, ok := p.facts[field]; ok {
		return desc.Type
	}
	return FieldTypeString
}
