package inspect

import (
	"errors"
	"fmt"
	"sort"
)

// Errors this package returns. Classify with [errors.Is].
var (
	// ErrAuthorizationInvalid reports an authorization decision that is not
	// usable on its own terms.
	ErrAuthorizationInvalid = errors.New("inspect: authorization decision is invalid")
	// ErrNotDisclosable reports a caller who may not learn that this instance
	// exists. It is deliberately indistinguishable, to the caller, from an
	// instance that does not exist.
	ErrNotDisclosable = errors.New("inspect: instance is not disclosable to this caller")
	// ErrInvalidRequest reports a request this package cannot project.
	ErrInvalidRequest = errors.New("inspect: request is invalid")
)

// Section is one stage of the inspector traversal. The stages are exactly the
// ones WF-RUN-019's GREEN clause names, and [TraversalOrder] is the order it
// names them in.
type Section string

// The traversal stages.
const (
	SectionDefinition  Section = "DEFINITION"
	SectionInstance    Section = "INSTANCE"
	SectionNode        Section = "NODE"
	SectionGovernance  Section = "GOVERNANCE"
	SectionTransaction Section = "TRANSACTION"
	SectionConnector   Section = "CONNECTOR"
	SectionObservation Section = "OBSERVATION"
	SectionTrace       Section = "TRACE"
)

// TraversalOrder returns the traversal in its declared order. A view renders
// every stage in this order, and a stage that was denied is named as denied
// rather than dropped from the sequence.
func TraversalOrder() []Section {
	return []Section{
		SectionDefinition, SectionInstance, SectionNode, SectionGovernance,
		SectionTransaction, SectionConnector, SectionObservation, SectionTrace,
	}
}

var declaredSections = func() map[Section]bool {
	m := make(map[Section]bool)
	for _, s := range TraversalOrder() {
		m[s] = true
	}
	return m
}()

// Validate reports whether s is a declared stage.
func (s Section) Validate() error {
	if !declaredSections[s] {
		return fmt.Errorf("%w: %q is not a traversal stage", ErrAuthorizationInvalid, string(s))
	}
	return nil
}

// Protected field tokens. These name the references that point at content a
// caller may be separately authorized for: the raw input, the produced
// artifact, the pinned context and the business subjects. Everything else in a
// view is operational metadata, which is what the inspector exists to show.
const (
	FieldInstanceInput    = "instance.input_ref"
	FieldInstanceContext  = "instance.effective_context_ref"
	FieldInstanceSubjects = "instance.business_subject_refs"
	FieldNodeInput        = "node.input_snapshot_ref"
	FieldNodeOutput       = "node.output_artifact_ref"
)

var redactableFields = map[string]bool{
	FieldInstanceInput:    true,
	FieldInstanceContext:  true,
	FieldInstanceSubjects: true,
	FieldNodeInput:        true,
	FieldNodeOutput:       true,
}

// RedactableFields returns the protected field tokens, sorted.
func RedactableFields() []string {
	out := make([]string, 0, len(redactableFields))
	for f := range redactableFields {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Effect is an authorization outcome.
type Effect uint8

// The two outcomes. The zero value is unspecified and fails validation, so a
// forgotten ruling is never read as an allowance.
const (
	EffectUnspecified Effect = iota
	EffectAllow
	EffectDeny
)

// String renders the effect token.
func (e Effect) String() string {
	switch e {
	case EffectAllow:
		return "ALLOW"
	case EffectDeny:
		return "DENY"
	default:
		return "EFFECT_UNSPECIFIED"
	}
}

// Valid reports whether e is a decided effect.
func (e Effect) Valid() bool { return e == EffectAllow || e == EffectDeny }

// Ruling is one decision plus the policy token explaining it.
type Ruling struct {
	Effect Effect
	Reason string
}

// Validate reports whether the ruling is usable. A denial without a reason is
// refused: a redaction the reader cannot attribute to a policy is
// indistinguishable from a bug.
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
// package as an input. This package never evaluates policy; it renders what a
// policy decided.
//
// InstanceDisclosable is separate from the section rulings for the reason
// internal/domains/intelligence gives about transactions: the existence of an
// execution is itself information, and a caller who may not know a promotion
// was ever started must not be able to infer it from a pattern of section
// denials.
type Authorization struct {
	PolicyVersion string
	Purpose       string
	Subject       string
	// InstanceDisclosable reports whether the caller may learn that this
	// instance exists at all.
	InstanceDisclosable bool
	// DenialReason is the policy token for a non-disclosable instance.
	DenialReason string
	// Sections is the per-stage ruling. A stage the decision is silent about
	// is denied: this package never invents an allowance it was not given.
	Sections map[Section]Ruling
	// Fields is the per-field ruling for the protected field tokens. A field
	// the decision is silent about is allowed only when its section is
	// allowed, matching ExplainTransaction's own rule.
	Fields map[string]Ruling
}

// Validate reports whether the decision is well formed on its own terms.
func (a Authorization) Validate() error {
	if a.PolicyVersion == "" {
		return fmt.Errorf("%w: policy version is required", ErrAuthorizationInvalid)
	}
	if a.Purpose == "" {
		return fmt.Errorf("%w: purpose of use is required", ErrAuthorizationInvalid)
	}
	if !a.InstanceDisclosable && a.DenialReason == "" {
		return fmt.Errorf("%w: a non-disclosable instance must carry a reason token", ErrAuthorizationInvalid)
	}
	for section, ruling := range a.Sections {
		if err := section.Validate(); err != nil {
			return err
		}
		if err := ruling.Validate(); err != nil {
			return fmt.Errorf("%w (section %s)", err, section)
		}
	}
	for field, ruling := range a.Fields {
		if !redactableFields[field] {
			return fmt.Errorf("%w: %q is not a redactable field", ErrAuthorizationInvalid, field)
		}
		if err := ruling.Validate(); err != nil {
			return fmt.Errorf("%w (field %s)", err, field)
		}
	}
	return nil
}

// AllowsSection reports whether the decision permits a stage. An absent ruling
// is a denial.
func (a Authorization) AllowsSection(s Section) bool {
	r, ok := a.Sections[s]
	return ok && r.Effect == EffectAllow
}

// sectionReason returns the denial token for a stage, or a default when the
// decision was simply silent about it.
func (a Authorization) sectionReason(s Section) string {
	if r, ok := a.Sections[s]; ok && r.Reason != "" {
		return r.Reason
	}
	return "NO_RULING_FOR_SECTION"
}

// AllowsField reports whether the decision permits a protected field. A field
// the decision is silent about follows its section.
func (a Authorization) AllowsField(field string, section Section) bool {
	if !a.AllowsSection(section) {
		return false
	}
	r, ok := a.Fields[field]
	if !ok {
		return true
	}
	return r.Effect == EffectAllow
}

// fieldReason returns the denial token for a field, falling back to its
// section's reason.
func (a Authorization) fieldReason(field string, section Section) string {
	if r, ok := a.Fields[field]; ok && r.Effect == EffectDeny && r.Reason != "" {
		return r.Reason
	}
	if !a.AllowsSection(section) {
		return a.sectionReason(section)
	}
	return "FIELD_DENIED"
}

// AllowAll builds a decision that permits every stage and every protected
// field. It exists so a fully authorized operator view — and a test of what
// the inspector renders when nothing is withheld — is one call rather than a
// hand-built map that could quietly omit a stage.
func AllowAll(policyVersion, purpose, subject string) Authorization {
	sections := make(map[Section]Ruling, len(declaredSections))
	for _, s := range TraversalOrder() {
		sections[s] = Ruling{Effect: EffectAllow}
	}
	fields := make(map[string]Ruling, len(redactableFields))
	for _, f := range RedactableFields() {
		fields[f] = Ruling{Effect: EffectAllow}
	}
	return Authorization{
		PolicyVersion:       policyVersion,
		Purpose:             purpose,
		Subject:             subject,
		InstanceDisclosable: true,
		Sections:            sections,
		Fields:              fields,
	}
}
