package observe

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PilotField is one field in the P1A Promotion observation contract.
type PilotField string

const (
	PilotFieldJobCode  PilotField = "job_code"
	PilotFieldGrade    PilotField = "grade"
	PilotFieldPosition PilotField = "position"
	PilotFieldBasePay  PilotField = "base_pay"

	// Short aliases keep call sites close to the promotion vocabulary.
	FieldJobCode  = PilotFieldJobCode
	FieldGrade    = PilotFieldGrade
	FieldPosition = PilotFieldPosition
	FieldBasePay  = PilotFieldBasePay
)

// AllPilotFields returns the closed pilot field set in stable order.
func AllPilotFields() []PilotField {
	return []PilotField{PilotFieldJobCode, PilotFieldGrade, PilotFieldPosition, PilotFieldBasePay}
}

// ComparisonVerdict is the three-valued result of comparing intended,
// canonical and observed values. UNKNOWN is not a failed MATCH: it means a
// source did not provide enough readable evidence to decide.
type ComparisonVerdict string

const (
	Match    ComparisonVerdict = "MATCH"
	Mismatch ComparisonVerdict = "MISMATCH"
	Unknown  ComparisonVerdict = "UNKNOWN"

	VerdictMatch    = Match
	VerdictMismatch = Mismatch
	VerdictUnknown  = Unknown
)

// PilotValue keeps a missing, redacted or unavailable field from collapsing
// into an empty string. Base pay is intentionally represented as the exact
// canonical text supplied by the domain (for example, "125000.00 USD"); this
// package performs no currency conversion or floating-point comparison.
type PilotValue struct {
	State  values.PresenceState
	Value  string
	Reason string
}

// Known creates a readable pilot value.
func Known(value string) PilotValue { return PilotValue{State: values.PresenceValue, Value: value} }

// UnknownValue creates an explicitly undecidable value.
func UnknownValue(reason string) PilotValue {
	return PilotValue{State: values.PresenceUnknown, Reason: reason}
}

// RedactedValue creates a value withheld by an authorization boundary.
func RedactedValue(reason string) PilotValue {
	return PilotValue{State: values.PresenceRedacted, Reason: reason}
}

// UnavailableValue creates a value that could not be retrieved.
func UnavailableValue(reason string) PilotValue {
	return PilotValue{State: values.PresenceUnavailable, Reason: reason}
}

// AbsentValue creates a field the source explicitly did not supply.
func AbsentValue() PilotValue { return PilotValue{State: values.PresenceAbsent} }

// Validate reports whether a pilot value carries an explicit presence state.
func (v PilotValue) Validate() error {
	if !v.State.Valid() {
		return fmt.Errorf("observe: pilot value has invalid presence state %q", v.State)
	}
	if v.State == values.PresenceValue && v.Reason != "" {
		return errors.New("observe: readable pilot value cannot carry a reason")
	}
	return nil
}

func (v PilotValue) readable() bool { return v.State == values.PresenceValue }

// PilotFields carries the three sides of the Promotion pilot's four-field
// comparison. Every member must be explicit, including an UNKNOWN member.
type PilotFields struct {
	JobCode  PilotValue
	Grade    PilotValue
	Position PilotValue
	BasePay  PilotValue
}

// NewPilotFields creates a fully readable pilot field set. Callers that do not
// have a readable value should use an explicit PilotValue constructor instead
// of passing an empty string.
func NewPilotFields(jobCode, grade, position, basePay string) PilotFields {
	return PilotFields{
		JobCode: Known(jobCode), Grade: Known(grade), Position: Known(position), BasePay: Known(basePay),
	}
}

// UnknownPilotFields creates a fully explicit undecidable field set.
func UnknownPilotFields(reason string) PilotFields {
	v := UnknownValue(reason)
	return PilotFields{JobCode: v, Grade: v, Position: v, BasePay: v}
}

// Validate checks all four fields and rejects a silent zero-value field.
func (p PilotFields) Validate() error {
	for _, item := range []struct {
		field PilotField
		value PilotValue
	}{
		{PilotFieldJobCode, p.JobCode}, {PilotFieldGrade, p.Grade},
		{PilotFieldPosition, p.Position}, {PilotFieldBasePay, p.BasePay},
	} {
		if err := item.value.Validate(); err != nil {
			return fmt.Errorf("observe: pilot field %s: %w", item.field, err)
		}
	}
	return nil
}

func (p PilotFields) value(field PilotField) PilotValue {
	switch field {
	case PilotFieldJobCode:
		return p.JobCode
	case PilotFieldGrade:
		return p.Grade
	case PilotFieldPosition:
		return p.Position
	case PilotFieldBasePay:
		return p.BasePay
	default:
		return PilotValue{}
	}
}

// FieldComparison is the evidence for one pilot field. Values are retained
// in all three columns so an operator can distinguish canonical drift from a
// provider delivery failure without consulting an unstructured log.
type FieldComparison struct {
	Field     PilotField
	Intended  PilotValue
	Canonical PilotValue
	Observed  PilotValue
	Verdict   ComparisonVerdict
	Reason    string
}

// PilotComparison is the aggregate comparison. MISMATCH dominates UNKNOWN,
// and UNKNOWN dominates MATCH, so an uncertain field can never be averaged
// into a false pass.
type PilotComparison struct {
	Verdict ComparisonVerdict
	Fields  []FieldComparison
}

// ComparePilotFields compares all four Promotion pilot fields.
func ComparePilotFields(intended, canonical, observed PilotFields) (PilotComparison, error) {
	for name, fields := range map[string]PilotFields{
		"intended": intended, "canonical": canonical, "observed": observed,
	} {
		if err := fields.Validate(); err != nil {
			return PilotComparison{}, fmt.Errorf("observe: %s pilot fields: %w", name, err)
		}
	}

	result := PilotComparison{Verdict: Match, Fields: make([]FieldComparison, 0, len(AllPilotFields()))}
	for _, field := range AllPilotFields() {
		item := FieldComparison{
			Field: field, Intended: intended.value(field), Canonical: canonical.value(field), Observed: observed.value(field),
			Verdict: Match,
		}
		if !item.Intended.readable() || !item.Canonical.readable() || !item.Observed.readable() {
			item.Verdict = Unknown
			item.Reason = unknownReason(item.Intended, item.Canonical, item.Observed)
		} else {
			var reasons []string
			if item.Intended.Value != item.Canonical.Value {
				reasons = append(reasons, "canonical differs from intended")
			}
			if item.Canonical.Value != item.Observed.Value {
				reasons = append(reasons, "observed differs from canonical")
			}
			if item.Intended.Value != item.Observed.Value {
				reasons = append(reasons, "observed differs from intended")
			}
			if len(reasons) > 0 {
				item.Verdict = Mismatch
				item.Reason = strings.Join(reasons, "; ")
			}
		}
		if item.Verdict == Mismatch {
			result.Verdict = Mismatch
		} else if item.Verdict == Unknown && result.Verdict == Match {
			result.Verdict = Unknown
		}
		result.Fields = append(result.Fields, item)
	}
	return result, nil
}

// Compare is the short form used by reconciliation policy ports.
func Compare(intended, canonical, observed PilotFields) (PilotComparison, error) {
	return ComparePilotFields(intended, canonical, observed)
}

func unknownReason(values ...PilotValue) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if !value.readable() {
			state := value.State.String()
			if value.Reason != "" {
				state += " (" + value.Reason + ")"
			}
			parts = append(parts, state)
		}
	}
	return "unreadable value: " + strings.Join(parts, ", ")
}

// Explain returns a bounded, deterministic account of the comparison.
func Explain(result PilotComparison) string {
	parts := make([]string, 0, len(result.Fields))
	for _, field := range result.Fields {
		part := string(field.Field) + "=" + string(field.Verdict)
		if field.Reason != "" {
			part += "(" + field.Reason + ")"
		}
		parts = append(parts, part)
	}
	return "promotion pilot reconciliation " + string(result.Verdict) + ": " + strings.Join(parts, ", ")
}
