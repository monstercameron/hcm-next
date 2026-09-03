package storagemanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// ColumnMapping is one physical column a property maps to. A property maps
// to more than one column when its Go type is itself a compound value —
// [model.TemporalEffectiveDated] intervals become two columns, and the five
// lifecycle dimensions become five.
type ColumnMapping struct {
	Name    string
	SQLType string
}

// PropertySQLMapping is one DB-003 row: everything the property-level
// generator needs to know to emit real DDL and to enforce the property's
// declared policies application-side where SQL alone cannot.
type PropertySQLMapping struct {
	PropertyRef      string
	Entity           string
	Columns          []ColumnMapping
	NotNull          bool
	DefaultBehavior  string
	EncryptionPolicy string
	SearchPolicy     string
	ConstraintNote   string
}

// PropertyMappingManifest is the full DB-003 output.
type PropertyMappingManifest struct {
	GeneratedBy    string
	RegistryDigest string
	Properties     []PropertySQLMapping
}

// Digest returns a stable sha256 digest of the manifest's own content.
func (m PropertyMappingManifest) Digest() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	w("REGISTRY", m.RegistryDigest)
	for _, p := range m.Properties {
		w("PROPERTY", p.PropertyRef, p.Entity, fmt.Sprint(p.NotNull), p.DefaultBehavior,
			p.EncryptionPolicy, p.SearchPolicy, p.ConstraintNote)
		for _, c := range p.Columns {
			w("COLUMN", c.Name, c.SQLType)
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// BuildPropertyMappings generates the DB-003 manifest: one SQL mapping per
// registered property. It fails with the offending property's reference,
// owning entity and schema path when a property's declared Go type has no
// known SQL mapping — an unsupported mapping is a generation error, not a
// silently skipped row.
func BuildPropertyMappings(reg *model.Registry) (PropertyMappingManifest, error) {
	props := reg.Properties()
	out := make([]PropertySQLMapping, 0, len(props))
	for _, p := range props {
		columns, err := columnsFor(p)
		if err != nil {
			return PropertyMappingManifest{}, fmt.Errorf(
				"storagemanifest: property %s (entity %s, schema %s): %w", p.Ref, p.Entity, p.SchemaPath, err)
		}
		out = append(out, PropertySQLMapping{
			PropertyRef:      string(p.Ref),
			Entity:           p.Entity.String(),
			Columns:          columns,
			NotNull:          p.Presence == model.PresenceRequired,
			DefaultBehavior:  defaultBehaviorFor(p.Presence),
			EncryptionPolicy: encryptionPolicyFor(p.Classification),
			SearchPolicy:     searchPolicyFor(p.Classification),
			ConstraintNote:   constraintNoteFor(p),
		})
	}
	return PropertyMappingManifest{
		GeneratedBy:    "tools/gen/storagemanifest (DB-003)",
		RegistryDigest: reg.Digest(),
		Properties:     out,
	}, nil
}

// columnsFor maps a property's declared Go type to its physical column set.
// The mapping is intentionally small and explicit: internal/intent/model's
// catalog uses a bounded set of primitive and shared kernel types (see
// internal/kernel/values and internal/intent/lifecycle), and an unmapped type
// is exactly the DB-003 RED case, not a type this function should guess at.
func columnsFor(p model.PropertyDefinition) ([]ColumnMapping, error) {
	base := p.Ref.Column()
	switch p.GoType {
	case "string":
		return []ColumnMapping{{Name: base, SQLType: "text"}}, nil
	case "uint32":
		return []ColumnMapping{{Name: base, SQLType: "integer"}}, nil
	case "bool":
		return []ColumnMapping{{Name: base, SQLType: "boolean"}}, nil
	case "[]string":
		return []ColumnMapping{{Name: base, SQLType: "text[]"}}, nil
	case "lifecycle.StateID":
		return []ColumnMapping{{Name: base, SQLType: "text"}}, nil
	case "lifecycle.Dimensions":
		return []ColumnMapping{
			{Name: base + "_request_state", SQLType: "text"},
			{Name: base + "_execution_state", SQLType: "text"},
			{Name: base + "_business_state", SQLType: "text"},
			{Name: base + "_consistency_state", SQLType: "text"},
			{Name: base + "_obligation_state", SQLType: "text"},
		}, nil
	case "values.Decimal":
		return []ColumnMapping{{Name: base, SQLType: "numeric(19,4)"}}, nil
	case "values.Instant":
		return []ColumnMapping{{Name: base, SQLType: "timestamptz"}}, nil
	case "values.EffectiveInterval":
		return []ColumnMapping{
			{Name: base + "_from", SQLType: "timestamptz"},
			{Name: base + "_to", SQLType: "timestamptz"},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported Go type %q has no SQL column mapping", p.GoType)
	}
}

func defaultBehaviorFor(p model.PresenceRule) string {
	switch p {
	case model.PresenceRequired:
		return "NONE_CALLER_MUST_SUPPLY"
	case model.PresenceOptional:
		return "NULLABLE_NO_DEFAULT"
	case model.PresenceConditional:
		return "APPLICATION_ENFORCED"
	default:
		return "UNKNOWN"
	}
}

func encryptionPolicyFor(c model.ClassificationLabel) string {
	switch c {
	case model.ClassPublic, model.ClassInternal:
		return "PLAINTEXT"
	default:
		return "ENCRYPTED_AT_REST"
	}
}

func searchPolicyFor(c model.ClassificationLabel) string {
	if encryptionPolicyFor(c) == "ENCRYPTED_AT_REST" {
		return "NONE"
	}
	return "EXACT_MATCH"
}

func constraintNoteFor(p model.PropertyDefinition) string {
	switch p.Correction {
	case model.CorrectionSupersedes:
		return "SUPERSEDES: prior row retained, new effective-dated row inserted; no UPDATE of the value in place"
	case model.CorrectionAppends:
		return "APPENDS_CORRECTION: correction is a new ledger_event; original row is immutable"
	case model.CorrectionNoCorrection:
		return "IMMUTABLE_NO_CORRECTION: column is write-once; no UPDATE, no compensating row"
	default:
		return "UNSPECIFIED"
	}
}
