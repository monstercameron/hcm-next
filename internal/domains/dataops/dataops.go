// Package dataops owns the HRIS administrator's read-only diagnostic
// surfaces: the effective-date debugger that explains how a field's value came
// to be what it is, and the cross-system diff that says where Human Capital Management Suite and an
// external system of record disagree.
//
// Semantic owner: DataOps and operations assurance. Phase: P1A.
//
// Everything here observes. Nothing here repairs. That separation is the
// product claim of P1A and it is enforced structurally: the comparison is a
// pure function, the drift detector counts its own effects and refuses to
// return a result it cannot certify as zero-effect, and the classification of
// a mismatch as "repair-safe" is a recommendation carried alongside the
// finding, never an action taken on it.
//
// The package holds no storage and no connector. It defines two read ports -
// a bitemporal field history and a page of external observations - and the
// intent kernel wires real implementations to them. A diagnostic that accepted
// facts from its caller would be able to certify whatever the caller wanted it
// to certify.
package dataops

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const (
	dataopsSchemaVer = 1
	authSchema       = "hcmnext.domains.dataops.Authorization"
)

// Shared DataOps errors. All are matchable with errors.Is.
var (
	// ErrFieldID is returned for a malformed field identifier.
	ErrFieldID = errors.New("dataops: field identifier is malformed")
	// ErrNoFieldsRequested is returned for a request with an empty projection.
	ErrNoFieldsRequested = errors.New("dataops: at least one field must be requested")
	// ErrDuplicateField is returned when a field is requested twice.
	ErrDuplicateField = errors.New("dataops: field requested more than once")
	// ErrAuthorizationInvalid is returned for a malformed authorization input.
	ErrAuthorizationInvalid = errors.New("dataops: authorization decision is malformed")
	// ErrAuthorizationIncomplete is returned when the decision does not rule
	// on every requested field. An unruled field is refused, never defaulted:
	// a diagnostic that guesses will eventually guess ALLOW.
	ErrAuthorizationIncomplete = errors.New("dataops: authorization decision does not rule on every requested field")
	// ErrRequestInvalid is returned for a malformed request.
	ErrRequestInvalid = errors.New("dataops: request is invalid")
	// ErrReaderFailed wraps a failure from a read port.
	ErrReaderFailed = errors.New("dataops: read port failed")
	// ErrPortWidenedProjection is returned when a port answers with a field
	// that was not asked for, or about a subject that was not asked about.
	ErrPortWidenedProjection = errors.New("dataops: read port answered outside the requested projection")
)

// FieldID names one field in a diagnostic. The vocabulary is open rather than
// a closed enum: the diff and the debugger are pointed at whatever fields a
// tenant's authority matrix covers, including fields no Human Capital Management Suite domain owns.
// The shape is still constrained, because a field identifier is part of an
// authorization decision and must be comparable as an exact token.
type FieldID string

// MaxFieldIDLength bounds a field identifier.
const MaxFieldIDLength = 128

// Validate reports whether f is a syntactically legal field identifier: one or
// more dot-separated segments of lowercase letters, digits and underscores.
func (f FieldID) Validate() error {
	s := string(f)
	if s == "" {
		return fmt.Errorf("%w: empty", ErrFieldID)
	}
	if len(s) > MaxFieldIDLength {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrFieldID, len(s), MaxFieldIDLength)
	}
	for _, segment := range strings.Split(s, ".") {
		if segment == "" {
			return fmt.Errorf("%w: %q has an empty segment", ErrFieldID, s)
		}
		for i := 0; i < len(segment); i++ {
			c := segment[i]
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
				continue
			}
			return fmt.Errorf("%w: %q has byte %#x", ErrFieldID, s, c)
		}
	}
	return nil
}

// String returns the field token.
func (f FieldID) String() string { return string(f) }

// normalizeFields validates, deduplicates and sorts a projection. The sort is
// what makes every downstream result and digest independent of the order the
// caller happened to list its fields in.
func normalizeFields(fields []FieldID) ([]FieldID, error) {
	if len(fields) == 0 {
		return nil, ErrNoFieldsRequested
	}
	seen := make(map[FieldID]struct{}, len(fields))
	out := make([]FieldID, 0, len(fields))
	for _, f := range fields {
		if err := f.Validate(); err != nil {
			return nil, err
		}
		if _, dup := seen[f]; dup {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateField, f)
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// Effect is one field-level authorization ruling.
type Effect uint8

// Effects.
const (
	// EffectUnspecified is the zero value and is never legal in a decision.
	EffectUnspecified Effect = iota
	// EffectAllow permits disclosing the field.
	EffectAllow
	// EffectDeny withholds the field. The field is still reported by name and
	// marked denied, so a denial stays distinguishable from an absent value.
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

// Ruling is the decision for one field plus the policy token explaining it.
// The reason is a token, never prose quoting the protected value.
type Ruling struct {
	Effect Effect
	Reason string
}

// Validate reports whether the ruling is usable.
func (r Ruling) Validate() error {
	if !r.Effect.Valid() {
		return fmt.Errorf("%w: effect is unspecified", ErrAuthorizationInvalid)
	}
	if r.Effect == EffectDeny && r.Reason == "" {
		return fmt.Errorf("%w: a denial must carry a reason token", ErrAuthorizationInvalid)
	}
	return nil
}

// Authorization is an already-evaluated authorization result handed to this
// package as an input.
//
// This package does not compute it, for the same reason People does not:
// a read model that decides its own authorization can widen its own
// visibility. SubjectDisclosable is separate from the field rulings because
// existence is itself protected - a caller who may not know a subject exists
// must not infer it from a per-field denial.
type Authorization struct {
	// PolicyVersion pins the evaluated policy so a result can be replayed.
	PolicyVersion string
	// Purpose is the declared purpose of use the decision was made under.
	Purpose string
	// SubjectDisclosable reports whether the caller may learn that the
	// subjects of this request exist at all.
	SubjectDisclosable bool
	// SubjectDenialReason is the policy token for a non-disclosable subject.
	SubjectDenialReason string
	// Fields is the per-field ruling. Every requested field must appear.
	Fields map[FieldID]Ruling
}

// Validate reports whether the decision is well formed on its own terms.
func (a Authorization) Validate() error {
	if a.PolicyVersion == "" {
		return fmt.Errorf("%w: policy version is required", ErrAuthorizationInvalid)
	}
	if a.Purpose == "" {
		return fmt.Errorf("%w: purpose of use is required", ErrAuthorizationInvalid)
	}
	if !a.SubjectDisclosable && a.SubjectDenialReason == "" {
		return fmt.Errorf("%w: a non-disclosable subject must carry a reason token", ErrAuthorizationInvalid)
	}
	for field, ruling := range a.Fields {
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
func (a Authorization) RulingFor(field FieldID) (Ruling, bool) {
	r, ok := a.Fields[field]
	return r, ok
}

// Allows reports whether the decision permits disclosing a field.
func (a Authorization) Allows(field FieldID) bool {
	r, ok := a.Fields[field]
	return ok && r.Effect == EffectAllow
}

// Covers reports whether the decision rules on every field, naming the first
// field it is silent about.
func (a Authorization) Covers(fields []FieldID) error {
	for _, f := range fields {
		if _, ok := a.Fields[f]; !ok {
			return fmt.Errorf("%w: no ruling for %s", ErrAuthorizationIncomplete, f)
		}
	}
	return nil
}

// AllowedFields returns the subset of fields the decision permits, in the
// order given.
func (a Authorization) AllowedFields(fields []FieldID) []FieldID {
	out := make([]FieldID, 0, len(fields))
	for _, f := range fields {
		if a.Allows(f) {
			out = append(out, f)
		}
	}
	return out
}

// Canonical returns the canonical byte encoding, or nil when invalid. Field
// rulings are emitted in sorted field order, never in map order.
func (a Authorization) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	fields := make([]FieldID, 0, len(a.Fields))
	for f := range a.Fields {
		fields = append(fields, f)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i] < fields[j] })

	w := canonicalbytes.New(authSchema, dataopsSchemaVer).
		String("policy_version", a.PolicyVersion).
		String("purpose", a.Purpose).
		Bool("subject_disclosable", a.SubjectDisclosable).
		String("subject_denial_reason", a.SubjectDenialReason).
		Count("fields", len(fields))
	for _, f := range fields {
		r := a.Fields[f]
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

// Access is the per-field disclosure outcome of a diagnostic.
type Access uint8

// Field access states.
const (
	// AccessUnspecified is the zero value and is never a legal result.
	AccessUnspecified Access = iota
	// AccessAuthorized means the field is disclosed.
	AccessAuthorized
	// AccessDenied means the field was requested and withheld. It still
	// appears in the result, named, so a refusal is not mistaken for absence.
	AccessDenied
)

var accessWire = map[Access]string{
	AccessAuthorized: "AUTHORIZED",
	AccessDenied:     "DENIED",
}

// String returns the stable wire token, or "ACCESS_UNSPECIFIED".
func (a Access) String() string {
	if s, ok := accessWire[a]; ok {
		return s
	}
	return "ACCESS_UNSPECIFIED"
}

// Disclosure is what the caller is being told at the whole-subject level.
type Disclosure uint8

// Disclosure states.
const (
	// DisclosureUnspecified is the zero value and is never a legal result.
	DisclosureUnspecified Disclosure = iota
	// DisclosureFull means every requested field was authorized.
	DisclosureFull
	// DisclosurePartial means at least one requested field was denied.
	DisclosurePartial
	// DisclosureWithheld means the caller may not learn anything about the
	// subject, including whether it exists.
	DisclosureWithheld
)

var disclosureWire = map[Disclosure]string{
	DisclosureFull:     "FULL",
	DisclosurePartial:  "PARTIAL",
	DisclosureWithheld: "WITHHELD",
}

// String returns the stable wire token, or "DISCLOSURE_UNSPECIFIED".
func (d Disclosure) String() string {
	if s, ok := disclosureWire[d]; ok {
		return s
	}
	return "DISCLOSURE_UNSPECIFIED"
}

// MaxNarrativeLines bounds every narrative this package produces, so that an
// explanation cannot become an unbounded data-export channel.
const MaxNarrativeLines = 64
