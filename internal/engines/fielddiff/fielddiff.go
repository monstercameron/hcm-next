// Package fielddiff is the pure comparison engine behind the cross-system
// field diff: given two canonicalised sides of the same field, it says how
// they relate and why, and it says nothing at all about what should be done
// about it.
//
// Semantic owner: shared-engines. Phase: P1A.
//
// The engine is deliberately ignorant of authority, freshness policy, tenancy
// and repair. Those are business decisions the DataOps domain owns; folding
// them in here would make the comparison unusable for any caller with a
// different policy, and would quietly turn a comparison into a
// recommendation. What this package owns is the one question a comparison can
// answer on its own: are these two values the same, and if not, does the
// evidence order them.
//
// A value the caller is not allowed to read, does not know, or could not
// retrieve is not compared at all. Compare refuses with ErrNotComparable
// rather than guessing, because "we could not look" and "they disagree" are
// different findings and only one of them justifies a repair.
package fielddiff

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	sideSchema     = "hcmnext.engines.fielddiff.Side"
	outcomeSchema  = "hcmnext.engines.fielddiff.Outcome"
	fielddiffSchVr = 1
)

// Comparison errors. All are matchable with errors.Is.
var (
	// ErrKindUnspecified is returned when a side does not declare the value
	// kind it is carrying. Two strings that happen to look alike are not the
	// same fact if one is a date and the other a code.
	ErrKindUnspecified = errors.New("fielddiff: side does not declare a value kind")
	// ErrSideInvalid is returned for a side whose presence is malformed.
	ErrSideInvalid = errors.New("fielddiff: side is malformed")
	// ErrNotComparable is returned when at least one side is UNKNOWN,
	// REDACTED or UNAVAILABLE. The caller must classify that case itself; the
	// engine will not report an epistemic gap as agreement or as a mismatch.
	ErrNotComparable = errors.New("fielddiff: at least one side cannot be read")
)

// Version reports this engine's own contract version. It changes whenever the
// relation vocabulary, the reason tokens or the comparison order change,
// because a stored outcome must remain interpretable by the rules that
// produced it.
func Version() int { return fielddiffSchVr }

// ValueKind is the declared type of a comparable value. It is part of the
// comparison input rather than inferred from the text, because inference is
// how "0001" and "1" become the same employee number.
type ValueKind string

// Value kinds P1A compares. The canonical text form of each is the caller's
// responsibility; this engine compares the text it is given.
const (
	// KindUnspecified is the zero value and is never legal.
	KindUnspecified ValueKind = ""
	// KindString is free text.
	KindString ValueKind = "STRING"
	// KindEnum is a closed-vocabulary token.
	KindEnum ValueKind = "ENUM"
	// KindDecimal is a fixed-scale decimal in canonical text form.
	KindDecimal ValueKind = "DECIMAL"
	// KindMoney is an amount and currency in canonical text form.
	KindMoney ValueKind = "MONEY"
	// KindDate is an ISO-8601 calendar date.
	KindDate ValueKind = "DATE"
	// KindBool is "true" or "false".
	KindBool ValueKind = "BOOL"
)

var kindWire = map[ValueKind]struct{}{
	KindString:  {},
	KindEnum:    {},
	KindDecimal: {},
	KindMoney:   {},
	KindDate:    {},
	KindBool:    {},
}

// Valid reports whether k is a declared value kind.
func (k ValueKind) Valid() bool { _, ok := kindWire[k]; return ok }

// String returns the kind token, or "VALUE_KIND_UNSPECIFIED".
func (k ValueKind) String() string {
	if k.Valid() {
		return string(k)
	}
	return "VALUE_KIND_UNSPECIFIED"
}

// Side is one half of a comparison: what the value is, whether it can be read
// at all, and when it last changed.
//
// UpdatedAt is optional and its absence is meaningful. Without it a
// disagreement can only be reported as a conflict; with it on both sides the
// engine can say which side moved last. It never invents an ordering from one
// timestamp, because "we changed at noon" says nothing about a side that has
// never said when it changed.
type Side struct {
	// Kind is the declared value type. Required on both sides.
	Kind ValueKind
	// Value is the canonical text of the value, or the reason there is none.
	Value values.Presence[string]
	// UpdatedAt is when this side last changed the field. Optional.
	UpdatedAt values.Instant
}

// Validate reports whether the side is well formed.
func (s Side) Validate() error {
	if !s.Kind.Valid() {
		return fmt.Errorf("%w: %q", ErrKindUnspecified, string(s.Kind))
	}
	if err := s.Value.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrSideInvalid, err)
	}
	if s.UpdatedAt.IsSet() {
		if err := s.UpdatedAt.Validate(); err != nil {
			return fmt.Errorf("%w: updated_at: %w", ErrSideInvalid, err)
		}
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (s Side) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	encoded, err := values.MarshalPresence(s.Value, values.StringCodec{})
	if err != nil {
		return nil
	}
	w := canonicalbytes.New(sideSchema, fielddiffSchVr).
		String("kind", s.Kind.String()).
		Field("value", encoded).
		Bool("updated_at?", s.UpdatedAt.IsSet())
	if s.UpdatedAt.IsSet() {
		w.Value("updated_at", s.UpdatedAt)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// readable reports whether the side carries a value that may be compared.
func (s Side) readable() bool { return s.Value.IsValue() }

// vacant reports whether the side asserts there is no value here: not supplied
// at all, or explicitly null. Both are answers about the field; neither is a
// refusal to answer.
func (s Side) vacant() bool {
	switch s.Value.State() {
	case values.PresenceAbsent, values.PresenceNull, values.PresenceNotApplicable:
		return true
	default:
		return false
	}
}

// Relation is how the two sides stand to each other. It is a statement about
// the values, never about what to do next.
type Relation uint8

// Relations. Left is the canonical side; right is the observed external side.
const (
	// RelationUnspecified is the zero value and is never a legal result.
	RelationUnspecified Relation = iota
	// RelationMatch means both sides carry the same value, or both agree that
	// there is no value.
	RelationMatch
	// RelationCanonicalAhead means the values differ and the canonical side
	// changed more recently.
	RelationCanonicalAhead
	// RelationExternalAhead means the values differ and the external side
	// changed more recently.
	RelationExternalAhead
	// RelationConflict means the values differ and the evidence does not order
	// them. This is the honest result when both sides moved at the same time,
	// or when at least one side cannot say when it moved.
	RelationConflict
	// RelationMissingLeft means the canonical side has no value and the
	// external side does.
	RelationMissingLeft
	// RelationMissingRight means the external side has no value and the
	// canonical side does.
	RelationMissingRight
	// RelationTypeMismatch means the two sides do not even claim to be the
	// same kind of value. Their texts are not comparable.
	RelationTypeMismatch
)

var relationWire = map[Relation]string{
	RelationMatch:          "MATCH",
	RelationCanonicalAhead: "CANONICAL_AHEAD",
	RelationExternalAhead:  "EXTERNAL_AHEAD",
	RelationConflict:       "CONFLICT",
	RelationMissingLeft:    "MISSING_LEFT",
	RelationMissingRight:   "MISSING_RIGHT",
	RelationTypeMismatch:   "TYPE_MISMATCH",
}

// String returns the stable wire token, or "RELATION_UNSPECIFIED".
func (r Relation) String() string {
	if s, ok := relationWire[r]; ok {
		return s
	}
	return "RELATION_UNSPECIFIED"
}

// Valid reports whether r is a legal relation.
func (r Relation) Valid() bool { _, ok := relationWire[r]; return ok }

// Agrees reports whether the relation means the two sides say the same thing.
func (r Relation) Agrees() bool { return r == RelationMatch }

// Reason tokens. They are stable identifiers, never prose, and never contain a
// compared value: a diff result travels further than the values it compared.
const (
	// ReasonValuesEqual is a match on equal canonical text.
	ReasonValuesEqual = "values_equal"
	// ReasonBothVacant is a match because neither side asserts a value.
	ReasonBothVacant = "both_sides_have_no_value"
	// ReasonCanonicalNewer orders a difference by the canonical update time.
	ReasonCanonicalNewer = "canonical_updated_after_external"
	// ReasonExternalNewer orders a difference by the external update time.
	ReasonExternalNewer = "external_updated_after_canonical"
	// ReasonSimultaneous is a difference with identical update times.
	ReasonSimultaneous = "values_differ_at_identical_update_time"
	// ReasonUnordered is a difference where at least one side has no update
	// time, so nothing orders them.
	ReasonUnordered = "values_differ_without_update_ordering"
	// ReasonCanonicalVacant is a canonical side with no value.
	ReasonCanonicalVacant = "canonical_side_has_no_value"
	// ReasonExternalVacant is an external side with no value.
	ReasonExternalVacant = "external_side_has_no_value"
	// ReasonKindsDiffer is a declared-kind disagreement.
	ReasonKindsDiffer = "declared_value_kinds_differ"
)

// Outcome is the engine's whole answer.
type Outcome struct {
	Relation Relation
	// Reason is the stable token explaining the relation.
	Reason string
	// Ordered reports whether an update-time ordering decided the relation. A
	// caller that will not act on an unordered difference can test this
	// without re-deriving it.
	Ordered bool
}

// Validate reports whether the outcome is well formed.
func (o Outcome) Validate() error {
	if !o.Relation.Valid() {
		return fmt.Errorf("fielddiff: outcome relation is unspecified")
	}
	if o.Reason == "" {
		return fmt.Errorf("fielddiff: outcome carries no reason token")
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (o Outcome) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(outcomeSchema, fielddiffSchVr).
		String("relation", o.Relation.String()).
		String("reason", o.Reason).
		Bool("ordered", o.Ordered).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Explain returns a bounded, value-free description of how the outcome was
// reached: what the engine concluded, on what token, and whether an update
// ordering decided it.
//
// It never contains a compared value. An explanation travels further than the
// data it explains - into tickets, dashboards and exports - and the field's
// authorization ruling does not travel with it.
func (o Outcome) Explain() []string {
	if o.Validate() != nil {
		return nil
	}
	lines := []string{
		"relation: " + o.Relation.String(),
		"reason: " + o.Reason,
	}
	if o.Ordered {
		lines = append(lines, "decided by the two update times")
	} else {
		lines = append(lines, "no update ordering was used")
	}
	return lines
}

// Compare relates the canonical side to the observed external side.
//
// It is a pure function of its two arguments: no clock, no map iteration, no
// I/O. The same pair always produces the same outcome, which is what makes a
// digest over a diff meaningful.
//
// The order of the checks is the contract. Kind disagreement outranks value
// comparison, because comparing the texts of two different types is a category
// error rather than a mismatch. Unreadable sides outrank everything else,
// because nothing true can be said about a value nobody was allowed to read.
func Compare(canonical, external Side) (Outcome, error) {
	if err := canonical.Validate(); err != nil {
		return Outcome{}, fmt.Errorf("fielddiff: canonical side: %w", err)
	}
	if err := external.Validate(); err != nil {
		return Outcome{}, fmt.Errorf("fielddiff: external side: %w", err)
	}
	if canonical.Kind != external.Kind {
		return Outcome{
			Relation: RelationTypeMismatch,
			Reason:   ReasonKindsDiffer,
		}, nil
	}
	if !canonical.readable() && !canonical.vacant() {
		return Outcome{}, fmt.Errorf("%w: canonical side is %s", ErrNotComparable, canonical.Value.State())
	}
	if !external.readable() && !external.vacant() {
		return Outcome{}, fmt.Errorf("%w: external side is %s", ErrNotComparable, external.Value.State())
	}

	switch {
	case canonical.vacant() && external.vacant():
		return Outcome{Relation: RelationMatch, Reason: ReasonBothVacant}, nil
	case canonical.vacant():
		return Outcome{Relation: RelationMissingLeft, Reason: ReasonCanonicalVacant}, nil
	case external.vacant():
		return Outcome{Relation: RelationMissingRight, Reason: ReasonExternalVacant}, nil
	}

	left := canonical.Value.MustValue()
	right := external.Value.MustValue()
	if left == right {
		return Outcome{Relation: RelationMatch, Reason: ReasonValuesEqual}, nil
	}

	if !canonical.UpdatedAt.IsSet() || !external.UpdatedAt.IsSet() {
		return Outcome{Relation: RelationConflict, Reason: ReasonUnordered}, nil
	}
	switch canonical.UpdatedAt.Compare(external.UpdatedAt) {
	case 1:
		return Outcome{Relation: RelationCanonicalAhead, Reason: ReasonCanonicalNewer, Ordered: true}, nil
	case -1:
		return Outcome{Relation: RelationExternalAhead, Reason: ReasonExternalNewer, Ordered: true}, nil
	default:
		return Outcome{Relation: RelationConflict, Reason: ReasonSimultaneous}, nil
	}
}
