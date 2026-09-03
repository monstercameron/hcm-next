package sources

import (
	"fmt"
	"regexp"
	"sort"
)

var (
	entityNamePattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	entityKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	authorityRefShape = regexp.MustCompile(`^authority\.[a-z][a-z0-9_]*/v[1-9][0-9]*$`)
	retentionRefShape = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	entityRefShape    = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*/v[1-9][0-9]*$`)
)

// forbiddenGoTypes are property go_type values the metamodel explicitly
// prohibits: wire-contract-primitives.md's "no map[string]any or untyped
// JSON at domain boundaries" and "Money/quantity arithmetic uses generated
// FixedDecimal helpers, never float64" (MSRC-002 RED: "float money" and
// "untyped IDs" must not compile).
var forbiddenGoTypes = map[string]string{
	"float32":                "money and quantity values use values.Decimal/values.Money, never a binary float",
	"float64":                "money and quantity values use values.Decimal/values.Money, never a binary float",
	"any":                    "no untyped JSON or interface{} at a domain boundary",
	"interface{}":            "no untyped JSON or interface{} at a domain boundary",
	"map[string]interface{}": "no untyped JSON or interface{} at a domain boundary",
	"map[string]any":         "no untyped JSON or interface{} at a domain boundary",
}

// Compile resolves every cross-reference in bundle against the bundle's own
// combined vocabulary (metamodel enums, authorities, retention classes,
// entities) and returns the compiled [Manifest] plus every
// [UnresolvedReferenceError] found. It never stops at the first error, for
// the same reason tools/gen/schemaflux.Compile does not: a source author
// fixing a batch of new entities benefits from seeing every problem at once.
//
// Compile returns a non-nil *Manifest even when errs is non-empty, but (per
// the sibling package's documented posture) that Manifest is not the
// publishable one in that case: a caller must treat any non-empty errs as
// "do not use this Manifest."
func Compile(bundle Bundle) (*Manifest, []error) {
	var errs []error
	vocab := NewVocabulary(bundle.Metamodel)

	// --- Authorities ---------------------------------------------------
	authByRef := map[string]AuthoritySource{}
	for _, a := range bundle.Authorities {
		if _, dup := authByRef[a.Ref]; dup {
			errs = append(errs, fmt.Errorf("%s:%d: duplicate authority ref %q", a.SourceFile, a.SourceLine, a.Ref))
			continue
		}
		authByRef[a.Ref] = a
		if !authorityRefShape.MatchString(a.Ref) {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: a.SourceFile, SourceLine: a.SourceLine,
				EntityName: a.Ref, Field: "ref", Value: a.Ref,
				Reason: "does not parse as authority.<domain_scope>/v<version>",
			})
		}
		if !vocab.AuthorityKind[a.Kind] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: a.SourceFile, SourceLine: a.SourceLine,
				EntityName: a.Ref, Field: "kind", Value: a.Kind,
				Reason: "not one of the metamodel's declared authority_kinds",
			})
		}
		if !vocab.MergePolicy[a.Merge] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: a.SourceFile, SourceLine: a.SourceLine,
				EntityName: a.Ref, Field: "merge", Value: a.Merge,
				Reason: "not one of the metamodel's declared merge_policies",
			})
		}
	}

	// --- Retention classes -----------------------------------------------
	retByRef := map[string]RetentionClassSource{}
	for _, r := range bundle.Retentions {
		if _, dup := retByRef[r.Ref]; dup {
			errs = append(errs, fmt.Errorf("%s:%d: duplicate retention class ref %q", r.SourceFile, r.SourceLine, r.Ref))
			continue
		}
		retByRef[r.Ref] = r
		if !retentionRefShape.MatchString(r.Ref) {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: r.SourceFile, SourceLine: r.SourceLine,
				EntityName: r.Ref, Field: "ref", Value: r.Ref,
				Reason: "does not parse as an UPPER_SNAKE_CASE retention class ref",
			})
		}
		if r.DefaultPeriodDays == 0 && len(r.JurisdictionOverrides) == 0 {
			errs = append(errs, fmt.Errorf("%s:%d: retention class %s declares no default period and no jurisdiction override",
				r.SourceFile, r.SourceLine, r.Ref))
		}
		if _, ok := authByRef[r.AuthorityRef]; !ok {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindAuthority, SourceFile: r.SourceFile, SourceLine: r.SourceLine,
				EntityName: r.Ref, Field: "authority_ref", Value: r.AuthorityRef,
				Reason: "does not resolve against any declared authority in schema/schemaflux/registries/v1",
			})
		}
	}

	// --- Entities --------------------------------------------------------
	entByRef := map[string]EntitySource{}
	entKeys := map[string]string{}
	for _, e := range bundle.Entities {
		ref := e.Ref()
		if _, dup := entByRef[ref]; dup {
			errs = append(errs, fmt.Errorf("%s:%d: duplicate entity ref %q", e.SourceFile, e.SourceLine, ref))
			continue
		}
		entByRef[ref] = e
		if existing, dup := entKeys[e.Key]; dup {
			errs = append(errs, fmt.Errorf("%s:%d: entity key %q used by both %s and %s",
				e.SourceFile, e.SourceLine, e.Key, existing, ref))
		} else {
			entKeys[e.Key] = ref
		}

		if !entityNamePattern.MatchString(e.Name) {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindKey, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
				EntityName: ref, Field: "name", Value: e.Name, Reason: "not a PascalCase entity name",
			})
		}
		if !entityKeyPattern.MatchString(e.Key) {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindKey, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
				EntityName: ref, Field: "key", Value: e.Key, Reason: "not a snake_case entity key",
			})
		}
		if !vocab.EntityClass[e.Class] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
				EntityName: ref, Field: "class", Value: e.Class,
				Reason: "not one of the metamodel's declared entity_classes",
			})
		}
		if !vocab.Status[e.Status] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
				EntityName: ref, Field: "status", Value: e.Status,
				Reason: "not one of the metamodel's declared definition_statuses",
			})
		}
		if !e.Covered && e.Status == "ACTIVE" {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
				EntityName: ref, Field: "status", Value: e.Status,
				Reason: "a covered:false entity (no funded Go registration) cannot declare status ACTIVE",
			})
		}
		if e.LifecycleAssignment == vocab.NoBusinessMarker && e.Class != "READ_MODEL" {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
				EntityName: ref, Field: "lifecycle_assignment", Value: e.LifecycleAssignment,
				Reason: "NO_BUSINESS_LIFECYCLE is reserved for class READ_MODEL",
			})
		}
		if e.LifecycleAssignment == "" {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
				EntityName: ref, Field: "lifecycle_assignment", Value: "", Reason: "declares no lifecycle assignment",
			})
		}
		if e.Class == "AGGREGATE_ROOT" && e.LifecycleAssignment != vocab.NoBusinessMarker {
			if e.CommandBoundary == "" || !vocab.Boundary[e.CommandBoundary] {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "command_boundary", Value: e.CommandBoundary,
					Reason: "an AGGREGATE_ROOT with a business lifecycle must declare one of the metamodel's consistency_boundaries",
				})
			} else if e.CommandBoundary == "CROSS_AGGREGATE_TRANSACTION" && len(e.Invariants) == 0 {
				errs = append(errs, fmt.Errorf(
					"%s:%d: %s declares CROSS_AGGREGATE_TRANSACTION with no invariants governing it",
					e.SourceFile, e.SourceLine, ref))
			}
		}

		seenProp := map[string]bool{}
		for _, p := range e.Properties {
			propRef := p.Ref(e.Key)
			if seenProp[propRef] {
				errs = append(errs, fmt.Errorf("%s:%d: %s: duplicate property %q", e.SourceFile, e.SourceLine, ref, propRef))
			}
			seenProp[propRef] = true

			if reason, bad := forbiddenGoTypes[p.GoType]; bad {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].go_type", Value: p.GoType, Reason: reason,
				})
			}
			if !vocab.Presence[p.Presence] {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].presence", Value: p.Presence,
					Reason: "not one of the metamodel's declared presence_rules",
				})
			}
			if !vocab.Classification[p.Classification] {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].classification", Value: p.Classification,
					Reason: "not one of the metamodel's declared classification_labels",
				})
			}
			if !vocab.Temporal[p.Temporal] {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].temporal", Value: p.Temporal,
					Reason: "not one of the metamodel's declared temporal_behaviors",
				})
			}
			if !vocab.Correction[p.Correction] {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].correction", Value: p.Correction,
					Reason: "not one of the metamodel's declared correction_behaviors",
				})
			}
			if !vocab.Status[p.Status] {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindVocabulary, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].status", Value: p.Status,
					Reason: "not one of the metamodel's declared definition_statuses",
				})
			}
			if p.Status == "ACTIVE" {
				a, ok := authByRef[p.AuthorityRef]
				if !ok {
					errs = append(errs, &UnresolvedReferenceError{
						Kind: ReferenceKindAuthority, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
						EntityName: ref, Field: "properties[" + p.Name + "].authority_ref", Value: p.AuthorityRef,
						Reason: "does not resolve against any declared authority",
					})
				} else if !a.Covered {
					errs = append(errs, &UnresolvedReferenceError{
						Kind: ReferenceKindAuthority, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
						EntityName: ref, Field: "properties[" + p.Name + "].authority_ref", Value: p.AuthorityRef,
						Reason: "an ACTIVE property cannot cite a covered:false (not yet Go-registered) authority",
					})
				}
			} else if _, ok := authByRef[p.AuthorityRef]; !ok {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindAuthority, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].authority_ref", Value: p.AuthorityRef,
					Reason: "does not resolve against any declared authority",
				})
			}
			if _, ok := retByRef[p.RetentionClassRef]; !ok {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindRetention, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "properties[" + p.Name + "].retention_class_ref", Value: p.RetentionClassRef,
					Reason: "does not resolve against any declared retention class",
				})
			}
		}
	}
	// Child refs resolved in a second pass, once every entity ref is known.
	for _, e := range bundle.Entities {
		ref := e.Ref()
		for _, c := range e.ChildRefs {
			child, ok := entByRef[c]
			if !ok {
				errs = append(errs, &UnresolvedReferenceError{
					Kind: ReferenceKindChild, SourceFile: e.SourceFile, SourceLine: e.SourceLine,
					EntityName: ref, Field: "child_refs", Value: c, Reason: "does not resolve against any declared entity",
				})
				continue
			}
			if child.Class != "CHILD" {
				errs = append(errs, fmt.Errorf("%s:%d: %s claims %s as a child, but its class is %s, not CHILD",
					e.SourceFile, e.SourceLine, ref, c, child.Class))
			}
		}
	}

	// --- Relationships -----------------------------------------------------
	relByRef := map[string]RelationshipSource{}
	for _, r := range bundle.Relationships {
		ref := r.Ref()
		if _, dup := relByRef[ref]; dup {
			errs = append(errs, fmt.Errorf("%s:%d: duplicate relationship ref %q", r.SourceFile, r.SourceLine, ref))
			continue
		}
		relByRef[ref] = r
		if !entityRefShape.MatchString(ref) {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: r.SourceFile, SourceLine: r.SourceLine,
				EntityName: ref, Field: "name", Value: r.Name, Reason: "not a PascalCase relationship name",
			})
		}
		if !vocab.Cardinality[r.Cardinality] {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindVocabulary, SourceFile: r.SourceFile, SourceLine: r.SourceLine,
				EntityName: ref, Field: "cardinality", Value: r.Cardinality,
				Reason: "not one of the metamodel's declared cardinalities",
			})
		}
		if _, ok := entByRef[r.SourceEntity]; !ok {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindEntity, SourceFile: r.SourceFile, SourceLine: r.SourceLine,
				EntityName: ref, Field: "source_entity", Value: r.SourceEntity,
				Reason: "does not resolve against any declared entity",
			})
		}
		if _, ok := entByRef[r.TargetEntity]; !ok {
			errs = append(errs, &UnresolvedReferenceError{
				Kind: ReferenceKindEntity, SourceFile: r.SourceFile, SourceLine: r.SourceLine,
				EntityName: ref, Field: "target_entity", Value: r.TargetEntity,
				Reason: "does not resolve against any declared entity",
			})
		}
	}

	familyCounts := map[string]int{}
	for _, e := range bundle.Entities {
		familyCounts[e.Family]++
	}

	sortedEntities := append([]EntitySource(nil), bundle.Entities...)
	sort.Slice(sortedEntities, func(i, j int) bool {
		if sortedEntities[i].Name != sortedEntities[j].Name {
			return sortedEntities[i].Name < sortedEntities[j].Name
		}
		return sortedEntities[i].Version < sortedEntities[j].Version
	})
	sortedRelationships := append([]RelationshipSource(nil), bundle.Relationships...)
	sort.Slice(sortedRelationships, func(i, j int) bool {
		if sortedRelationships[i].Name != sortedRelationships[j].Name {
			return sortedRelationships[i].Name < sortedRelationships[j].Name
		}
		return sortedRelationships[i].Version < sortedRelationships[j].Version
	})
	sortedAuthorities := append([]AuthoritySource(nil), bundle.Authorities...)
	sort.Slice(sortedAuthorities, func(i, j int) bool { return sortedAuthorities[i].Ref < sortedAuthorities[j].Ref })
	sortedRetentions := append([]RetentionClassSource(nil), bundle.Retentions...)
	sort.Slice(sortedRetentions, func(i, j int) bool { return sortedRetentions[i].Ref < sortedRetentions[j].Ref })

	return &Manifest{
		Entities:      sortedEntities,
		Relationships: sortedRelationships,
		Authorities:   sortedAuthorities,
		Retentions:    sortedRetentions,
		FamilyCounts:  familyCounts,
	}, errs
}
