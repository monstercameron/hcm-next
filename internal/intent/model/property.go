package model

// PresenceRule declares whether a property must, may, or conditionally
// carries a value.
type PresenceRule string

// Presence rules.
const (
	PresenceRequired    PresenceRule = "REQUIRED"
	PresenceOptional    PresenceRule = "OPTIONAL"
	PresenceConditional PresenceRule = "CONDITIONAL"
)

// Valid reports whether p is one of the three declared presence rules.
func (p PresenceRule) Valid() bool {
	switch p {
	case PresenceRequired, PresenceOptional, PresenceConditional:
		return true
	default:
		return false
	}
}

// TemporalBehavior declares how a property's value relates to business time.
type TemporalBehavior string

// Temporal behaviors.
const (
	TemporalEffectiveDated TemporalBehavior = "EFFECTIVE_DATED"
	TemporalPointInTime    TemporalBehavior = "POINT_IN_TIME"
	TemporalImmutable      TemporalBehavior = "IMMUTABLE"
)

// Valid reports whether t is one of the three declared temporal behaviors.
func (t TemporalBehavior) Valid() bool {
	switch t {
	case TemporalEffectiveDated, TemporalPointInTime, TemporalImmutable:
		return true
	default:
		return false
	}
}

// CorrectionBehavior declares how a property is corrected once recorded.
type CorrectionBehavior string

// Correction behaviors.
const (
	CorrectionSupersedes   CorrectionBehavior = "SUPERSEDES"
	CorrectionAppends      CorrectionBehavior = "APPENDS_CORRECTION"
	CorrectionNoCorrection CorrectionBehavior = "IMMUTABLE_NO_CORRECTION"
)

// Valid reports whether c is one of the three declared correction behaviors.
func (c CorrectionBehavior) Valid() bool {
	switch c {
	case CorrectionSupersedes, CorrectionAppends, CorrectionNoCorrection:
		return true
	default:
		return false
	}
}

// PropertyDefinition is the canonical property registry entry (MODEL-011).
//
// Every field here is required at publication: a property without a concrete
// type, presence rule, authority reference, temporal behavior, classification
// label, correction behavior or retention-class reference cannot be
// registered. This is the literal RED clause of MODEL-011.
type PropertyDefinition struct {
	Ref PropertyRef

	// Entity is the owning entity's reference. Ref.EntityKey() must equal
	// that entity's Key.
	Entity EntityRef

	// GoType is the concrete Go type this property resolves to, e.g.
	// "string", "values.Decimal", "lifecycle.StateID".
	GoType string

	// SchemaPath is the SchemaFlux/Protobuf field path this property binds
	// to, e.g. "hcmnext.people.v1.Employment.status".
	SchemaPath string

	Presence PresenceRule

	Classification ClassificationLabel

	Temporal TemporalBehavior

	// AuthorityRef names the [SourceAuthorityAssignment] governing writes to
	// this property.
	AuthorityRef string

	Correction CorrectionBehavior

	// RetentionClassRef names the [RetentionClass] this property's evidence
	// inherits.
	RetentionClassRef string

	Status DefinitionStatus
}

// Validate rejects a property definition missing concrete type, presence,
// authority, temporal, classification, correction or retention semantics.
func (p PropertyDefinition) Validate() error {
	if err := p.Ref.Validate(); err != nil {
		return err
	}
	if err := p.Entity.Validate(); err != nil {
		return err
	}
	if p.GoType == "" {
		return newError("PropertyDefinition.Validate", "go_type", ErrInvalidProperty,
			"%s declares no concrete type", p.Ref)
	}
	if p.SchemaPath == "" {
		return newError("PropertyDefinition.Validate", "schema_path", ErrInvalidProperty,
			"%s declares no schema path", p.Ref)
	}
	if !p.Presence.Valid() {
		return newError("PropertyDefinition.Validate", "presence", ErrInvalidProperty,
			"%s has presence %q, outside the three declared rules", p.Ref, p.Presence)
	}
	if !p.Classification.Valid() {
		return newError("PropertyDefinition.Validate", "classification", ErrInvalidProperty,
			"%s has classification %q, outside the declared labels", p.Ref, p.Classification)
	}
	if !p.Temporal.Valid() {
		return newError("PropertyDefinition.Validate", "temporal", ErrInvalidProperty,
			"%s has temporal behavior %q, outside the three declared behaviors", p.Ref, p.Temporal)
	}
	if p.AuthorityRef == "" {
		return newError("PropertyDefinition.Validate", "authority_ref", ErrInvalidProperty,
			"%s names no source-authority assignment", p.Ref)
	}
	if !p.Correction.Valid() {
		return newError("PropertyDefinition.Validate", "correction", ErrInvalidProperty,
			"%s has correction behavior %q, outside the three declared behaviors", p.Ref, p.Correction)
	}
	if p.RetentionClassRef == "" {
		return newError("PropertyDefinition.Validate", "retention_class_ref", ErrInvalidProperty,
			"%s names no retention class", p.Ref)
	}
	if !p.Status.Valid() {
		return newError("PropertyDefinition.Validate", "status", ErrInvalidProperty,
			"%s has status %q, outside the four declared statuses", p.Ref, p.Status)
	}
	return nil
}
