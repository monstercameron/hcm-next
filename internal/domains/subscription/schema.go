package subscription

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidEventSchema = errors.New("subscription: invalid outbound event schema")
	ErrSchemaBreaking     = errors.New("subscription: outbound schema change is breaking")
	ErrInvalidEnvelope    = errors.New("subscription: invalid canonical envelope")
)

// FieldClassification is the closed classification vocabulary attached to
// each outbound field.
type FieldClassification string

const (
	ClassificationPublic       FieldClassification = "PUBLIC"
	ClassificationInternal     FieldClassification = "INTERNAL"
	ClassificationConfidential FieldClassification = "CONFIDENTIAL"
	ClassificationRestricted   FieldClassification = "RESTRICTED"
)

// FieldType is the stable logical type recorded in an outbound schema.
type FieldType string

const (
	TypeString  FieldType = "STRING"
	TypeBoolean FieldType = "BOOLEAN"
	TypeInteger FieldType = "INTEGER"
	TypeNumber  FieldType = "NUMBER"
	TypeRef     FieldType = "REFERENCE"
	TypeInstant FieldType = "INSTANT"
)

// SchemaField is one member of a closed event field set. Optional fields may
// be added compatibly; required additions are breaking.
type SchemaField struct {
	Name           string
	Type           FieldType
	Classification FieldClassification
	Optional       bool
}

// EventSchema freezes the field set for one event kind and version.
type EventSchema struct {
	Kind    EventKind
	Version int
	Fields  []SchemaField
	Digest  string
}

// NewEventSchema creates a versioned, closed schema and digests its exact
// field set. Field order supplied by a caller does not affect the digest.
func NewEventSchema(kind EventKind, schemaVersion int, fields []SchemaField) (EventSchema, error) {
	s := EventSchema{Kind: kind, Version: schemaVersion, Fields: cloneSchemaFields(fields)}
	if err := s.validate(); err != nil {
		return EventSchema{}, err
	}
	s.Digest = schemaDigest(s)
	return s, nil
}

// Verify detects mutation after a schema was published.
func (s EventSchema) Verify() error {
	if err := s.validate(); err != nil {
		return err
	}
	if s.Digest == "" || s.Digest != schemaDigest(s) {
		return fmt.Errorf("%w: schema digest does not match %s@%d", ErrInvalidEventSchema, s.Kind, s.Version)
	}
	return nil
}

// SchemaCompatibility is the INTENT-028-style result for a schema pair.
type SchemaCompatibility struct {
	Compatible    bool
	BreakingField string
	Reason        string
}

// CheckSchemaCompatibility compares closed field sets. Removed and
// type-changed fields are breaking and identify the offending field; an
// optional field addition is compatible.
func CheckSchemaCompatibility(previous, next EventSchema) (SchemaCompatibility, error) {
	if err := previous.Verify(); err != nil {
		return SchemaCompatibility{}, err
	}
	if err := next.Verify(); err != nil {
		return SchemaCompatibility{}, err
	}
	if previous.Kind != next.Kind || next.Version <= previous.Version {
		return SchemaCompatibility{}, fmt.Errorf("%w: versions must be increasing for the same event kind", ErrInvalidEventSchema)
	}
	oldFields := schemaFieldsByName(previous.Fields)
	newFields := schemaFieldsByName(next.Fields)
	for name, old := range oldFields {
		current, ok := newFields[name]
		if !ok {
			return SchemaCompatibility{BreakingField: name, Reason: "field_removed"}, fmt.Errorf("%w: field %q was removed", ErrSchemaBreaking, name)
		}
		if current.Type != old.Type {
			return SchemaCompatibility{BreakingField: name, Reason: "field_type_changed"}, fmt.Errorf("%w: field %q type changed from %s to %s", ErrSchemaBreaking, name, old.Type, current.Type)
		}
		if current.Classification != old.Classification {
			return SchemaCompatibility{BreakingField: name, Reason: "field_classification_changed"}, fmt.Errorf("%w: field %q classification changed from %s to %s", ErrSchemaBreaking, name, old.Classification, current.Classification)
		}
		if old.Optional && !current.Optional {
			return SchemaCompatibility{BreakingField: name, Reason: "field_became_required"}, fmt.Errorf("%w: field %q became required", ErrSchemaBreaking, name)
		}
	}
	for name, current := range newFields {
		if _, ok := oldFields[name]; !ok && !current.Optional {
			return SchemaCompatibility{BreakingField: name, Reason: "required_field_added"}, fmt.Errorf("%w: required field %q was added", ErrSchemaBreaking, name)
		}
	}
	return SchemaCompatibility{Compatible: true, Reason: "optional_fields_only"}, nil
}

// CompareSchemaVersions is a descriptive alias for
// [CheckSchemaCompatibility].
func CompareSchemaVersions(previous, next EventSchema) (SchemaCompatibility, error) {
	return CheckSchemaCompatibility(previous, next)
}

// SchemaRegistry retains published versions without replacing an existing
// kind/version pair. It is an in-memory pure adapter, not durable storage.
type SchemaRegistry struct {
	schemas map[EventKind]map[int]EventSchema
}

func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{schemas: make(map[EventKind]map[int]EventSchema)}
}

// Register publishes a schema version once and verifies compatibility with
// the latest prior version for that event kind.
func (r *SchemaRegistry) Register(s EventSchema) error {
	if r == nil {
		return ErrInvalidEventSchema
	}
	if err := s.Verify(); err != nil {
		return err
	}
	if r.schemas[s.Kind] == nil {
		r.schemas[s.Kind] = make(map[int]EventSchema)
	}
	if _, exists := r.schemas[s.Kind][s.Version]; exists {
		return fmt.Errorf("%w: %s@%d already exists", ErrInvalidEventSchema, s.Kind, s.Version)
	}
	var prior EventSchema
	for _, candidate := range r.schemas[s.Kind] {
		if prior.Version < candidate.Version {
			prior = candidate
		}
	}
	if prior.Version != 0 {
		if _, err := CheckSchemaCompatibility(prior, s); err != nil {
			return err
		}
	}
	r.schemas[s.Kind][s.Version] = cloneSchema(s)
	return nil
}

// Schema retrieves a detached published schema.
func (r *SchemaRegistry) Schema(kind EventKind, schemaVersion int) (EventSchema, bool) {
	if r == nil {
		return EventSchema{}, false
	}
	s, ok := r.schemas[kind][schemaVersion]
	if !ok {
		return EventSchema{}, false
	}
	return cloneSchema(s), true
}

// CanonicalEnvelope is the minimally disclosed outbound event envelope. The
// payload itself is never present; only its digest crosses this boundary.
type CanonicalEnvelope struct {
	Tenant        string
	Kind          EventKind
	SchemaVersion int
	SubjectRefs   []string
	EffectiveAt   time.Time
	KnownAt       time.Time
	PayloadDigest string
	ProvenanceRef string
	Sequence      uint64
}

// Validate checks the envelope shape and rejects wall-clock-dependent zero
// times, duplicate references, and absent digest/provenance evidence.
func (e CanonicalEnvelope) Validate() error {
	if strings.TrimSpace(e.Tenant) == "" || strings.TrimSpace(e.Tenant) != e.Tenant || !e.Kind.Valid() || e.SchemaVersion <= 0 {
		return fmt.Errorf("%w: tenant, event kind and schema version are required", ErrInvalidEnvelope)
	}
	if len(e.SubjectRefs) == 0 {
		return fmt.Errorf("%w: at least one subject reference is required", ErrInvalidEnvelope)
	}
	if err := validateScopeStrings(e.SubjectRefs, "subject reference"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if e.EffectiveAt.IsZero() || e.KnownAt.IsZero() {
		return fmt.Errorf("%w: effective-at and known-at are required", ErrInvalidEnvelope)
	}
	if strings.TrimSpace(e.PayloadDigest) == "" || strings.TrimSpace(e.ProvenanceRef) == "" || strings.TrimSpace(e.PayloadDigest) != e.PayloadDigest || strings.TrimSpace(e.ProvenanceRef) != e.ProvenanceRef {
		return fmt.Errorf("%w: payload digest and provenance reference are required", ErrInvalidEnvelope)
	}
	if e.Sequence == 0 {
		return fmt.Errorf("%w: sequence must be positive", ErrInvalidEnvelope)
	}
	return nil
}

// CanonicalBytes emits the envelope through the shared canonicalbytes writer.
// Times are normalized to UTC RFC3339Nano strings and subject references are
// framed as a sorted set.
func (e CanonicalEnvelope) CanonicalBytes() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	w := canonicalbytes.New("hcmnext.domains.subscription.CanonicalEnvelope", 1).
		String("tenant", e.Tenant).String("event_kind", string(e.Kind)).
		Int("schema_version", int64(e.SchemaVersion)).
		SortedStrings("subject_ref", e.SubjectRefs).
		String("effective_at", e.EffectiveAt.UTC().Format(time.RFC3339Nano)).
		String("known_at", e.KnownAt.UTC().Format(time.RFC3339Nano)).
		String("payload_digest", e.PayloadDigest).String("provenance_ref", e.ProvenanceRef).
		Int("sequence", int64(e.Sequence))
	return w.Bytes()
}

// Digest returns the canonical envelope digest, or an empty string for an
// invalid envelope.
func (e CanonicalEnvelope) Digest() string {
	b, err := e.CanonicalBytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Explain returns a redaction-safe envelope summary.
func (e CanonicalEnvelope) Explain() string {
	return fmt.Sprintf("subscription envelope kind=%s schema=%d tenant=%s sequence=%d digest=%s", e.Kind, e.SchemaVersion, e.Tenant, e.Sequence, e.Digest())
}

func (s EventSchema) validate() error {
	if !s.Kind.Valid() || s.Version <= 0 {
		return fmt.Errorf("%w: event kind and positive version are required", ErrInvalidEventSchema)
	}
	if len(s.Fields) == 0 {
		return fmt.Errorf("%w: field set cannot be empty", ErrInvalidEventSchema)
	}
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if strings.TrimSpace(field.Name) == "" || strings.TrimSpace(field.Name) != field.Name || field.Name == "*" || !field.Type.valid() || !field.Classification.valid() {
			return fmt.Errorf("%w: field %q is not a closed typed classified field", ErrInvalidEventSchema, field.Name)
		}
		if _, ok := seen[field.Name]; ok {
			return fmt.Errorf("%w: duplicate field %q", ErrInvalidEventSchema, field.Name)
		}
		seen[field.Name] = struct{}{}
	}
	return nil
}

func (t FieldType) valid() bool {
	switch t {
	case TypeString, TypeBoolean, TypeInteger, TypeNumber, TypeRef, TypeInstant:
		return true
	default:
		return false
	}
}

func (c FieldClassification) valid() bool {
	switch c {
	case ClassificationPublic, ClassificationInternal, ClassificationConfidential, ClassificationRestricted:
		return true
	default:
		return false
	}
}

func schemaDigest(s EventSchema) string {
	w := canonicalbytes.New("hcmnext.domains.subscription.EventSchema", 1).
		String("event_kind", string(s.Kind)).Int("version", int64(s.Version))
	fields := cloneSchemaFields(s.Fields)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	for _, field := range fields {
		w.String("field", field.Name).String("type", string(field.Type)).
			String("classification", string(field.Classification)).Bool("optional", field.Optional)
	}
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func schemaFieldsByName(fields []SchemaField) map[string]SchemaField {
	out := make(map[string]SchemaField, len(fields))
	for _, field := range fields {
		out[field.Name] = field
	}
	return out
}

func cloneSchemaFields(fields []SchemaField) []SchemaField {
	return append([]SchemaField(nil), fields...)
}

func cloneSchema(s EventSchema) EventSchema {
	s.Fields = cloneSchemaFields(s.Fields)
	return s
}
