package people

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// Authorization errors. All are matchable with errors.Is.
var (
	// ErrAuthorizationIncomplete is returned when the decision does not rule on
	// every requested field. The explanation refuses rather than treating an
	// unruled field as denied or, worse, as allowed.
	ErrAuthorizationIncomplete = errors.New("people: authorization decision does not rule on every requested field")
	// ErrAuthorizationInvalid is returned for a malformed decision.
	ErrAuthorizationInvalid = errors.New("people: authorization decision is malformed")
)

// Effect is one field-level ruling.
type Effect uint8

// Effects.
const (
	// EffectUnspecified is the zero value and is never legal in a decision.
	EffectUnspecified Effect = iota
	// EffectAllow permits disclosing the field's value and provenance.
	EffectAllow
	// EffectDeny withholds the value. The field is still reported, marked
	// denied, so that a caller can tell "you may not see this" from "there is
	// nothing here".
	EffectDeny
)

var effectWire = map[Effect]string{
	EffectAllow: "ALLOW",
	EffectDeny:  "DENY",
}

// String returns the stable wire token, or "EFFECT_UNSPECIFIED".
func (e Effect) String() string {
	if s, ok := effectWire[e]; ok {
		return s
	}
	return "EFFECT_UNSPECIFIED"
}

// Valid reports whether e is a legal effect.
func (e Effect) Valid() bool { _, ok := effectWire[e]; return ok }

// FieldRuling is the decision for one field plus the reason token that
// explains it. The reason is a policy token, never free text quoting the
// protected value.
type FieldRuling struct {
	Effect Effect
	Reason string
}

// Validate reports whether the ruling is usable.
func (r FieldRuling) Validate() error {
	if !r.Effect.Valid() {
		return fmt.Errorf("%w: effect is unspecified", ErrAuthorizationInvalid)
	}
	if r.Effect == EffectDeny && r.Reason == "" {
		return fmt.Errorf("%w: a denial must carry a reason token", ErrAuthorizationInvalid)
	}
	return nil
}

// AuthorizationDecision is an already-evaluated authorization result handed to
// this package as an input.
//
// This package does not compute it. AuthZ evaluates tenant, organization,
// legal entity, relationship, population, field, purpose and effective time;
// duplicating any of that here would create a second, weaker evaluator that
// drifts from the real one.
//
// SubjectDisclosable is separate from the field rulings because existence is
// itself protected: a caller who may not know the worker exists must not be
// able to infer it from a per-field denial.
type AuthorizationDecision struct {
	// PolicyVersion pins the evaluated policy so a result can be replayed.
	PolicyVersion string
	// Purpose is the declared purpose of use the decision was made under.
	Purpose string
	// SubjectDisclosable reports whether the caller may learn that this
	// subject exists at all.
	SubjectDisclosable bool
	// SubjectDenialReason is the policy token for a non-disclosable subject.
	SubjectDenialReason string
	// Fields is the per-field ruling. Every requested field must appear.
	Fields map[FieldID]FieldRuling
}

// Validate reports whether the decision is well formed on its own terms.
func (d AuthorizationDecision) Validate() error {
	if d.PolicyVersion == "" {
		return fmt.Errorf("%w: policy version is required", ErrAuthorizationInvalid)
	}
	if d.Purpose == "" {
		return fmt.Errorf("%w: purpose of use is required", ErrAuthorizationInvalid)
	}
	if !d.SubjectDisclosable && d.SubjectDenialReason == "" {
		return fmt.Errorf("%w: a non-disclosable subject must carry a reason token", ErrAuthorizationInvalid)
	}
	for field, ruling := range d.Fields {
		if err := field.Validate(); err != nil {
			return err
		}
		if err := ruling.Validate(); err != nil {
			return fmt.Errorf("%w (field %s)", err, field)
		}
	}
	return nil
}

// RulingFor returns the ruling for a field. The second result is false when
// the decision is silent about it, which callers must treat as a refusal to
// answer rather than as a default.
func (d AuthorizationDecision) RulingFor(field FieldID) (FieldRuling, bool) {
	r, ok := d.Fields[field]
	return r, ok
}

// Covers reports whether the decision rules on every field in fields, naming
// the first field it is silent about.
func (d AuthorizationDecision) Covers(fields []FieldID) error {
	for _, f := range fields {
		if _, ok := d.Fields[f]; !ok {
			return fmt.Errorf("%w: no ruling for %s", ErrAuthorizationIncomplete, f)
		}
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid. Field
// rulings are emitted in sorted field order, never in map order.
func (d AuthorizationDecision) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	fields := make([]FieldID, 0, len(d.Fields))
	for f := range d.Fields {
		fields = append(fields, f)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i] < fields[j] })

	w := canonicalbytes.New("hcmnext.domains.people.AuthorizationDecision", peopleSchemaVer).
		String("policy_version", d.PolicyVersion).
		String("purpose", d.Purpose).
		Bool("subject_disclosable", d.SubjectDisclosable).
		String("subject_denial_reason", d.SubjectDenialReason).
		Count("fields", len(fields))
	for _, f := range fields {
		r := d.Fields[f]
		w.String("field", string(f)).
			String("field.effect", r.Effect.String()).
			String("field.reason", r.Reason)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
