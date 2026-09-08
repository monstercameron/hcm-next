package cba

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type CompositionOutcome string

const (
	CompositionAllow                CompositionOutcome = "ALLOW"
	CompositionAllowWithObligations CompositionOutcome = "ALLOW_WITH_OBLIGATIONS"
	CompositionBlock                CompositionOutcome = "BLOCK"
	CompositionUnknown              CompositionOutcome = "UNKNOWN"
)

type ConstraintKind string

const (
	ConstraintWage       ConstraintKind = "WAGE"
	ConstraintSchedule   ConstraintKind = "SCHEDULE"
	ConstraintLeave      ConstraintKind = "LEAVE"
	ConstraintSeniority  ConstraintKind = "SENIORITY"
	ConstraintDiscipline ConstraintKind = "DISCIPLINE"
)

type ConstraintSource string

const (
	SourceStatute   ConstraintSource = "STATUTE"
	SourceAgreement ConstraintSource = "AGREEMENT"
	SourceCompany   ConstraintSource = "COMPANY"
)

var (
	ErrInvalidConstraint = errors.New("cba: invalid constraint")
	ErrInvalidPolicy     = errors.New("cba: invalid composition policy")
)

type CBAConstraint struct {
	ID, ClauseRef, ReleaseRef string
	Kind                      ConstraintKind
	Source                    ConstraintSource
	Mandatory, Prohibition    bool
	Value                     string
	Minimum, Maximum          int64
	HasMinimum, HasMaximum    bool
	Unit                      string
	KnownAt                   time.Time
	ServiceResultRevision     string
}

func (c CBAConstraint) valid() error {
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.ClauseRef) == "" || strings.TrimSpace(c.ReleaseRef) == "" {
		return fmt.Errorf("%w: id and provenance are required", ErrInvalidConstraint)
	}
	switch c.Kind {
	case ConstraintWage, ConstraintSchedule, ConstraintLeave, ConstraintSeniority, ConstraintDiscipline:
	default:
		return fmt.Errorf("%w: kind %q", ErrInvalidConstraint, c.Kind)
	}
	switch c.Source {
	case SourceStatute, SourceAgreement, SourceCompany:
	default:
		return fmt.Errorf("%w: source %q", ErrInvalidConstraint, c.Source)
	}
	if (!c.HasMinimum && c.Minimum != 0) || (!c.HasMaximum && c.Maximum != 0) {
		return fmt.Errorf("%w: bound value requires an explicit presence flag", ErrInvalidConstraint)
	}
	if c.Minimum < 0 || c.Maximum < 0 {
		return fmt.Errorf("%w: negative bounds are not permitted", ErrInvalidConstraint)
	}
	if c.HasMinimum && c.HasMaximum && c.Maximum < c.Minimum {
		return fmt.Errorf("%w: maximum below minimum", ErrInvalidConstraint)
	}
	if (c.HasMinimum || c.HasMaximum) && strings.TrimSpace(c.Unit) == "" {
		return fmt.Errorf("%w: bounded constraint requires a unit", ErrInvalidConstraint)
	}
	return nil
}

type ComparisonRule string

const (
	CompareHigherMinimum ComparisonRule = "HIGHER_MINIMUM"
	CompareLowerMaximum  ComparisonRule = "LOWER_MAXIMUM"
)

type CompositionPolicy struct{ Wage, Schedule, Leave, Seniority, Discipline ComparisonRule }

func (p CompositionPolicy) rule(k ConstraintKind) ComparisonRule {
	switch k {
	case ConstraintWage:
		return p.Wage
	case ConstraintSchedule:
		return p.Schedule
	case ConstraintLeave:
		return p.Leave
	case ConstraintSeniority:
		return p.Seniority
	default:
		return p.Discipline
	}
}
func (p CompositionPolicy) valid() bool {
	for _, k := range allConstraintKinds() {
		r := p.rule(k)
		if r != CompareHigherMinimum && r != CompareLowerMaximum {
			return false
		}
	}
	return true
}

// ProvenancePin is release metadata supplied by the trusted composition root.
type ProvenancePin struct {
	Source     ConstraintSource
	ReleaseRef string
	ClauseRefs []string
}

type CompositionRequest struct {
	Applicability            ApplicabilityResult
	KnownAt                  time.Time
	SeniorityServiceRevision string
	Constraints              []CBAConstraint
	Policy                   CompositionPolicy
	Provenance               []ProvenancePin
}

type CompositionObligation struct {
	ID, Kind, ClauseRef, ReleaseRef string
	Source                          ConstraintSource
	Mandatory                       bool
}
type CompositionCalculation struct {
	Kind, Unit                                string
	Minimum, Maximum                          int64
	HasMinimum, HasMaximum                    bool
	MinimumSourceClause, MinimumSourceRelease string
	MaximumSourceClause, MaximumSourceRelease string
}
type CompositionResult struct {
	Outcome                   CompositionOutcome
	Obligations, Prohibitions []CompositionObligation
	Calculations              []CompositionCalculation
	Reason                    string
}

func (r CompositionResult) Validate() error {
	switch r.Outcome {
	case CompositionAllow, CompositionAllowWithObligations, CompositionBlock, CompositionUnknown:
		return nil
	default:
		return fmt.Errorf("cba: invalid composition outcome %q", r.Outcome)
	}
}

func allConstraintKinds() [5]ConstraintKind {
	return [5]ConstraintKind{ConstraintWage, ConstraintSchedule, ConstraintLeave, ConstraintSeniority, ConstraintDiscipline}
}

func ComposeConstraints(q CompositionRequest) (CompositionResult, error) {
	out := CompositionResult{Outcome: CompositionUnknown}
	if !completeApplicable(q.Applicability) {
		out.Reason = "applicability lacks complete revision evidence"
		return out, nil
	}
	if !q.Policy.valid() {
		return out, ErrInvalidPolicy
	}
	if q.KnownAt.IsZero() {
		out.Reason = "knowledge time is required"
		return out, nil
	}
	if len(q.Constraints) == 0 {
		out.Reason = "no constraints were supplied"
		return out, nil
	}
	seenIDs := make(map[string]struct{}, len(q.Constraints))
	for _, c := range q.Constraints {
		if err := c.valid(); err != nil {
			return out, err
		}
		if _, exists := seenIDs[c.ID]; exists {
			return out, fmt.Errorf("%w: duplicate id %q", ErrInvalidConstraint, c.ID)
		}
		seenIDs[c.ID] = struct{}{}
		if !pinned(c, q.Provenance) {
			out.Reason = "constraint provenance is not pinned"
			return out, nil
		}
		if c.Kind == ConstraintSeniority {
			if c.KnownAt.IsZero() || !c.KnownAt.Equal(q.KnownAt) || q.SeniorityServiceRevision == "" || c.ServiceResultRevision != q.SeniorityServiceRevision {
				out.Reason = "seniority result time or service revision is stale or unpinned"
				return out, nil
			}
		}
		i := CompositionObligation{c.ID, string(c.Kind), c.ClauseRef, c.ReleaseRef, c.Source, c.Mandatory}
		if c.Prohibition {
			out.Prohibitions = append(out.Prohibitions, i)
		} else {
			out.Obligations = append(out.Obligations, i)
		}
	}
	for _, k := range allConstraintKinds() {
		calc, conflict, err := composeCalculation(k, q.Constraints, q.Policy.rule(k))
		if err != nil {
			return out, err
		}
		if conflict {
			out.Outcome = CompositionBlock
			out.Reason = "constraint ranges conflict"
			return sortedResult(out), nil
		}
		if calc != nil {
			out.Calculations = append(out.Calculations, *calc)
		}
	}
	if len(out.Prohibitions) > 0 {
		out.Outcome = CompositionBlock
		out.Reason = "a prohibition applies"
	} else {
		out.Outcome = CompositionAllowWithObligations
	}
	return sortedResult(out), nil
}

func completeApplicable(a ApplicabilityResult) bool {
	return a.Outcome == Applicable && a.AgreementID != "" && a.AgreementRevision != "" && a.UnitID != "" && a.UnitRevision != "" && a.MembershipID != "" && a.MembershipRevision != "" && a.Representative != "" && a.Source != "" && a.PrecedenceBasis != ""
}
func pinned(c CBAConstraint, pins []ProvenancePin) bool {
	for _, p := range pins {
		if p.Source == c.Source && p.ReleaseRef == c.ReleaseRef {
			for _, clause := range p.ClauseRefs {
				if clause == c.ClauseRef {
					return true
				}
			}
		}
	}
	return false
}

func composeCalculation(k ConstraintKind, all []CBAConstraint, rule ComparisonRule) (*CompositionCalculation, bool, error) {
	var candidates []CBAConstraint
	var mandatoryMin, mandatoryMax int64
	var hasMandatoryMin, hasMandatoryMax bool
	var mandatoryMinSource, mandatoryMaxSource *CBAConstraint
	for _, c := range all {
		if c.Kind != k || c.Prohibition || !c.HasMinimum && !c.HasMaximum {
			continue
		}
		if len(candidates) > 0 && c.Unit != candidates[0].Unit {
			return nil, false, fmt.Errorf("%w: mixed units for %s", ErrInvalidConstraint, k)
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return nil, false, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	for i := range candidates {
		c := candidates[i]
		if c.Mandatory {
			if c.HasMinimum && (!hasMandatoryMin || c.Minimum > mandatoryMin || c.Minimum == mandatoryMin && c.ID < mandatoryMinSource.ID) {
				mandatoryMin = c.Minimum
				hasMandatoryMin = true
				copy := c
				mandatoryMinSource = &copy
			}
			if c.HasMaximum && (!hasMandatoryMax || c.Maximum < mandatoryMax || c.Maximum == mandatoryMax && c.ID < mandatoryMaxSource.ID) {
				mandatoryMax = c.Maximum
				hasMandatoryMax = true
				copy := c
				mandatoryMaxSource = &copy
			}
		}
	}
	if hasMandatoryMin && hasMandatoryMax && mandatoryMin > mandatoryMax {
		return nil, true, nil
	}
	chosen := candidates[0]
	for _, c := range candidates[1:] {
		if rule == CompareHigherMinimum && c.HasMinimum && (!chosen.HasMinimum || c.Minimum > chosen.Minimum) || rule == CompareLowerMaximum && c.HasMaximum && (!chosen.HasMaximum || c.Maximum < chosen.Maximum) {
			chosen = c
		}
	}
	min, max := chosen.Minimum, chosen.Maximum
	hasMin, hasMax := chosen.HasMinimum, chosen.HasMaximum
	minSource, maxSource := chosen, chosen
	if hasMandatoryMin && (!hasMin || min < mandatoryMin) {
		min = mandatoryMin
		hasMin = true
		minSource = *mandatoryMinSource
	}
	if hasMandatoryMax && (!hasMax || max > mandatoryMax) {
		max = mandatoryMax
		hasMax = true
		maxSource = *mandatoryMaxSource
	}
	if hasMin && hasMax && min > max {
		return nil, true, nil
	}
	calculation := &CompositionCalculation{Kind: string(k), Unit: chosen.Unit, Minimum: min, Maximum: max, HasMinimum: hasMin, HasMaximum: hasMax}
	if hasMin {
		calculation.MinimumSourceClause, calculation.MinimumSourceRelease = minSource.ClauseRef, minSource.ReleaseRef
	}
	if hasMax {
		calculation.MaximumSourceClause, calculation.MaximumSourceRelease = maxSource.ClauseRef, maxSource.ReleaseRef
	}
	return calculation, false, nil
}

func sortedResult(r CompositionResult) CompositionResult {
	sort.Slice(r.Obligations, func(i, j int) bool { return r.Obligations[i].ID < r.Obligations[j].ID })
	sort.Slice(r.Prohibitions, func(i, j int) bool { return r.Prohibitions[i].ID < r.Prohibitions[j].ID })
	return r
}
