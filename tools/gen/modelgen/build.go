package modelgen

import (
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// FieldModel is one generated struct field: the property it comes from, the
// resolved Go type, the field name [pascalCase] derived for it, and whether
// [isImmutableWrite] marks it a property no drafted definition may write.
type FieldModel struct {
	Property  model.PropertyDefinition
	Type      typeInfo
	FieldName string
	Immutable bool
}

// EntityModel is one generated struct type: the source entity, the Go type
// name [entityTypeName] derived for it, and its fields sorted by
// PropertyRef.
type EntityModel struct {
	Entity   model.EntityDefinition
	TypeName string
	Fields   []FieldModel
}

// RelationshipModel is one generated relationship row.
type RelationshipModel struct {
	Def model.RelationshipDefinition
}

// ModelSet is the full, pure intermediate representation [Render] turns into
// Go source. It carries nothing that varies run to run: SourceDigest and
// every slice below are computed from sorted registry output only.
type ModelSet struct {
	// SourceDigest is [model.Registry.Digest] of the registry this set was
	// built from. It is embedded into the generated Registry so a caller can
	// detect at runtime whether the compiled internal/intent/model catalog
	// has drifted since generation (see registry.go's New).
	SourceDigest string

	Entities      []EntityModel
	Relationships []RelationshipModel
}

// Build compiles reg into a [ModelSet]. It is a pure function: the same
// registry always yields the same ModelSet, field order included, because
// every slice here is built from the registry's own sorted accessors
// ([model.Registry.Entities], [model.Registry.Properties],
// [model.Registry.Relationships]) rather than map iteration.
//
// Build fails closed in three cases, all MSRC-007 RED clauses: a property
// names a Go type outside [typeTable] (never falls back to `any`), two
// entities would generate the same Go type name, or two properties on one
// entity would generate the same Go field name.
func Build(reg *model.Registry) (*ModelSet, error) {
	if reg == nil {
		return nil, fmt.Errorf("modelgen: Build: nil registry")
	}

	propsByEntity := map[model.EntityRef][]model.PropertyDefinition{}
	for _, p := range reg.Properties() {
		propsByEntity[p.Entity] = append(propsByEntity[p.Entity], p)
	}

	ms := &ModelSet{SourceDigest: reg.Digest()}
	typeNames := map[string]model.EntityRef{}

	for _, e := range reg.Entities() {
		typeName := entityTypeName(e.Ref)
		if prior, dup := typeNames[typeName]; dup {
			return nil, fmt.Errorf(
				"modelgen: entities %s and %s both generate Go type name %q; add an explicit rename",
				prior, e.Ref, typeName)
		}
		typeNames[typeName] = e.Ref

		props := append([]model.PropertyDefinition(nil), propsByEntity[e.Ref]...)
		sort.Slice(props, func(i, j int) bool { return props[i].Ref < props[j].Ref })

		fieldNames := map[string]model.PropertyRef{}
		fields := make([]FieldModel, 0, len(props))
		for _, p := range props {
			info, err := resolveGoType(p.GoType)
			if err != nil {
				return nil, fmt.Errorf("modelgen: entity %s property %s: %w", e.Ref, p.Ref, err)
			}
			fieldName := pascalCase(p.Ref.Path())
			if fieldName == "" {
				return nil, fmt.Errorf("modelgen: property %s resolves to an empty Go field name", p.Ref)
			}
			if prior, dup := fieldNames[fieldName]; dup {
				return nil, fmt.Errorf(
					"modelgen: entity %s properties %s and %s both generate field name %q; add an explicit rename",
					e.Ref, prior, p.Ref, fieldName)
			}
			fieldNames[fieldName] = p.Ref

			fields = append(fields, FieldModel{
				Property:  p,
				Type:      info,
				FieldName: fieldName,
				Immutable: isImmutableWrite(e, p),
			})
		}

		ms.Entities = append(ms.Entities, EntityModel{Entity: e, TypeName: typeName, Fields: fields})
	}

	for _, r := range reg.Relationships() {
		ms.Relationships = append(ms.Relationships, RelationshipModel{Def: r})
	}

	return ms, nil
}

// entityTypeName derives the generated Go type name for an entity reference:
// the bare PascalCase name at version 1 (every entity in the compiled
// catalog today), or "<Name>V<Version>" for a later material version, so two
// versions of one entity can never collide on one Go identifier.
func entityTypeName(ref model.EntityRef) string {
	if ref.Version == 1 {
		return ref.Name
	}
	return fmt.Sprintf("%sV%d", ref.Name, ref.Version)
}

// isImmutableWrite decides MSRC-009's "a property the model marks immutable":
// a property that is both IMMUTABLE_NO_CORRECTION and IMMUTABLE-dated, on an
// entity that is not itself EVIDENCE-class.
//
// EVIDENCE-class entities (ApprovalBinding, ExecutionBinding, EvidenceRecord,
// Observation) are exactly the append-only records a CHANGE_REQUEST's
// EffectRefs create: [github.com/monstercameron/hcm-next/internal/intent/definitions.Bindings]'s
// approveProposalBinding and rejectProposalBinding legitimately declare
// WriteProperties on approval_binding.decision et al., which is
// IMMUTABLE_NO_CORRECTION/IMMUTABLE on ApprovalBinding — that single
// creation write is the whole point of evidence, not a correction. A
// non-EVIDENCE aggregate root's own IMMUTABLE_NO_CORRECTION property, by
// contrast — proposal_revision.material_digest is the one instance in the
// compiled catalog today — is a content-addressed identity fact the
// platform itself fixes at creation; no drafted CHANGE_REQUEST binding
// writes it, and [internal/intent/modelbinding] refuses one that tries.
func isImmutableWrite(e model.EntityDefinition, p model.PropertyDefinition) bool {
	if e.Class == model.ClassEvidence {
		return false
	}
	return p.Temporal == model.TemporalImmutable && p.Correction == model.CorrectionNoCorrection
}
