package scenario

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrBaselineMismatch = errors.New("scenario: baseline snapshot does not match revision")
	ErrBaselineScope    = errors.New("scenario: baseline scope does not match revision")
	ErrAccessEvidence   = errors.New("scenario: baseline access evidence is required")
	ErrUnapprovedField  = errors.New("scenario: field is not approved for read")
	ErrDuplicateDelta   = errors.New("scenario: duplicate delta")
)

// Baseline is a detached, read-only snapshot. Fields not in ApprovedFields are
// intentionally never inspected or returned by Evaluate.
type Baseline struct {
	SnapshotRef       string
	Scope             string
	PolicySnapshotRef string
	AccessDecisionRef string
	Fields            map[string]TypedValue
	ApprovedFields    []string
}

// Delta is a typed replacement in hypothetical state. It is not a domain
// event, command, outbox effect, or permission to write authoritative state.
type Delta struct {
	Field string
	Value TypedValue
}

func (d Delta) Validate() error {
	if strings.TrimSpace(d.Field) == "" {
		return fmt.Errorf("%w: empty field", ErrInvalidScenario)
	}
	return d.Value.Validate()
}

type Unknown struct {
	Field  string
	Reason string
}

type FieldResult struct {
	Field string
	Value TypedValue
	Known bool
}

type Evaluation struct {
	BaselineSnapshotRef string
	Scope               string
	PolicySnapshotRef   string
	AccessDecisionRef   string
	Fields              []FieldResult
	Unknowns            []Unknown
}

// Evaluate applies typed deltas to a pinned baseline in memory. The approved
// field list is an allowlist: an unapproved delta is rejected and unapproved
// baseline values cannot influence the result. Missing approved inputs become
// explicit unknowns rather than fabricated values.
func Evaluate(revision ScenarioRevision, baseline Baseline, deltas ...Delta) (Evaluation, error) {
	if err := revision.Validate(); err != nil {
		return Evaluation{}, err
	}
	if strings.TrimSpace(baseline.SnapshotRef) == "" || baseline.SnapshotRef != revision.BaselineSnapshotRef {
		return Evaluation{}, ErrBaselineMismatch
	}
	if strings.TrimSpace(baseline.Scope) == "" || baseline.Scope != revision.Scope {
		return Evaluation{}, ErrBaselineScope
	}
	if strings.TrimSpace(baseline.PolicySnapshotRef) == "" || strings.TrimSpace(baseline.AccessDecisionRef) == "" {
		return Evaluation{}, ErrAccessEvidence
	}
	approved := make(map[string]struct{}, len(baseline.ApprovedFields))
	for _, field := range baseline.ApprovedFields {
		if strings.TrimSpace(field) == "" {
			return Evaluation{}, fmt.Errorf("%w: empty approved field", ErrInvalidScenario)
		}
		if _, exists := approved[field]; exists {
			return Evaluation{}, fmt.Errorf("%w: duplicate approved field %q", ErrInvalidScenario, field)
		}
		approved[field] = struct{}{}
	}
	values := make(map[string]TypedValue, len(baseline.Fields))
	for field, value := range baseline.Fields {
		if _, ok := approved[field]; !ok {
			continue
		}
		if err := value.Validate(); err != nil {
			return Evaluation{}, fmt.Errorf("%w: baseline field %q: %v", ErrInvalidScenario, field, err)
		}
		values[field] = value
	}
	changed := make(map[string]struct{}, len(deltas))
	for _, delta := range deltas {
		if err := delta.Validate(); err != nil {
			return Evaluation{}, err
		}
		if _, ok := approved[delta.Field]; !ok {
			return Evaluation{}, fmt.Errorf("%w: %q", ErrUnapprovedField, delta.Field)
		}
		if _, ok := changed[delta.Field]; ok {
			return Evaluation{}, fmt.Errorf("%w: %q", ErrDuplicateDelta, delta.Field)
		}
		changed[delta.Field] = struct{}{}
		values[delta.Field] = delta.Value
	}
	fields := make([]string, 0, len(approved))
	for field := range approved {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	result := Evaluation{
		BaselineSnapshotRef: baseline.SnapshotRef,
		Scope:               baseline.Scope,
		PolicySnapshotRef:   baseline.PolicySnapshotRef,
		AccessDecisionRef:   baseline.AccessDecisionRef,
	}
	for _, field := range fields {
		value, ok := values[field]
		result.Fields = append(result.Fields, FieldResult{Field: field, Value: value, Known: ok})
		if !ok {
			result.Unknowns = append(result.Unknowns, Unknown{Field: field, Reason: "baseline value unavailable"})
		}
	}
	return result, nil
}
