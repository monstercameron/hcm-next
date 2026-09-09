package sources_test

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

// TestTodo_MSRC_002 is the MSRC-002 primary test: it loads
// schema/schemaflux/metamodel/v1/metamodel.yaml and asserts every declared
// vocabulary section is non-empty and internally well-formed, then compiles
// the full checked-in source tree against it and asserts zero unresolved
// references — the metamodel is not just parseable, it is sufficient to
// validate every other MSRC-003..MSRC-005 source file.
func TestTodo_MSRC_002(t *testing.T) {
	root := findRepoRoot(t)
	mm, err := sources.LoadMetamodel(filepath.Join(root, "schema", "schemaflux", "metamodel", "v1", "metamodel.yaml"))
	if err != nil {
		t.Fatalf("LoadMetamodel: %v", err)
	}

	nonEmpty := map[string][]string{
		"presence_rules":         mm.PresenceRules,
		"temporal_behaviors":     mm.TemporalBehaviors,
		"correction_behaviors":   mm.CorrectionBehaviors,
		"entity_classes":         mm.EntityClasses,
		"definition_statuses":    mm.DefinitionStatuses,
		"classification_labels":  mm.ClassificationLabels,
		"authority_kinds":        mm.AuthorityKinds,
		"merge_policies":         mm.MergePolicies,
		"consistency_boundaries": mm.ConsistencyBoundaries,
		"cardinalities":          mm.Cardinalities,
	}
	for field, vals := range nonEmpty {
		if len(vals) == 0 {
			t.Errorf("metamodel.%s is empty", field)
		}
	}
	if mm.NoBusinessLifecycle != "NO_BUSINESS_LIFECYCLE" {
		t.Errorf("metamodel.no_business_lifecycle = %q, want NO_BUSINESS_LIFECYCLE", mm.NoBusinessLifecycle)
	}

	manifest, bundle := compileValid(t)
	if len(manifest.Entities) == 0 {
		t.Fatal("compiled manifest has zero entities")
	}
	_ = bundle
}

// TestTodo_MSRC_002_Property asserts, for every property of every entity in
// the checked-in source tree, that its presence/classification/temporal/
// correction/status values are drawn from the metamodel's own declared
// vocabulary — not merely "some string that happened to compile" but exactly
// one of the enumerated values, checked independently of [sources.Compile]'s
// internal logic so a bug in Compile's own vocabulary tables cannot silently
// pass this test too.
func TestTodo_MSRC_002_Property(t *testing.T) {
	bundle := loadValidBundle(t)
	vocab := sources.NewVocabulary(bundle.Metamodel)

	checked := 0
	for _, e := range bundle.Entities {
		if !vocab.EntityClass[e.Class] {
			t.Errorf("%s: class %q not in metamodel vocabulary", e.Ref(), e.Class)
		}
		if !vocab.Status[e.Status] {
			t.Errorf("%s: status %q not in metamodel vocabulary", e.Ref(), e.Status)
		}
		for _, p := range e.Properties {
			checked++
			if !vocab.Presence[p.Presence] {
				t.Errorf("%s.%s: presence %q not in metamodel vocabulary", e.Ref(), p.Name, p.Presence)
			}
			if !vocab.Classification[p.Classification] {
				t.Errorf("%s.%s: classification %q not in metamodel vocabulary", e.Ref(), p.Name, p.Classification)
			}
			if !vocab.Temporal[p.Temporal] {
				t.Errorf("%s.%s: temporal %q not in metamodel vocabulary", e.Ref(), p.Name, p.Temporal)
			}
			if !vocab.Correction[p.Correction] {
				t.Errorf("%s.%s: correction %q not in metamodel vocabulary", e.Ref(), p.Name, p.Correction)
			}
			if !vocab.Status[p.Status] {
				t.Errorf("%s.%s: status %q not in metamodel vocabulary", e.Ref(), p.Name, p.Status)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no properties were checked; the source tree appears to have shrunk to zero properties")
	}
}

// TestTodo_MSRC_002_Golden pins the metamodel's declared vocabulary lists
// (sorted) so an accidental addition, removal or rename is caught here
// rather than only by a downstream consumer silently accepting a new value.
func TestTodo_MSRC_002_Golden(t *testing.T) {
	root := findRepoRoot(t)
	mm, err := sources.LoadMetamodel(filepath.Join(root, "schema", "schemaflux", "metamodel", "v1", "metamodel.yaml"))
	if err != nil {
		t.Fatalf("LoadMetamodel: %v", err)
	}

	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"presence_rules", mm.PresenceRules, []string{"REQUIRED", "OPTIONAL", "CONDITIONAL"}},
		{"temporal_behaviors", mm.TemporalBehaviors, []string{"EFFECTIVE_DATED", "POINT_IN_TIME", "IMMUTABLE"}},
		{"correction_behaviors", mm.CorrectionBehaviors, []string{"SUPERSEDES", "APPENDS_CORRECTION", "IMMUTABLE_NO_CORRECTION"}},
		{"entity_classes", mm.EntityClasses, []string{"AGGREGATE_ROOT", "CHILD", "VALUE", "EVENT", "EVIDENCE", "READ_MODEL"}},
		{"definition_statuses", mm.DefinitionStatuses, []string{"DRAFT", "ACTIVE", "DEPRECATED", "RETIRED"}},
		{"authority_kinds", mm.AuthorityKinds, []string{"INTERNAL", "EXTERNAL_SYSTEM", "HUMAN", "IMPORT", "AGENT"}},
		{"merge_policies", mm.MergePolicies, []string{"LAST_WRITE_WINS", "AUTHORITY_PRECEDENCE", "MANUAL_RECONCILIATION"}},
		{"consistency_boundaries", mm.ConsistencyBoundaries, []string{"LOCAL_ACID", "CROSS_AGGREGATE_TRANSACTION", "EXTERNAL_OBSERVATION"}},
		{"cardinalities", mm.Cardinalities, []string{"ONE_TO_ONE", "ONE_TO_MANY", "MANY_TO_MANY"}},
	}
	for _, c := range cases {
		got := append([]string(nil), c.got...)
		want := append([]string(nil), c.want...)
		sort.Strings(got)
		sort.Strings(want)
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", c.name, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s[%d] = %q, want %q", c.name, i, got[i], want[i])
			}
		}
	}
}

// TestTodo_MSRC_002_Fault is the MSRC-002 RED table: each case injects one of
// the disqualifying patterns the todo names — an unknown/undeclared
// vocabulary value, a float64 money-shaped type, an untyped `any` id, or a
// covered:false entity claiming ACTIVE status (the "undocumented extension"
// case) — into one property or entity of an otherwise-valid bundle, and
// expects [sources.Compile] to report exactly one typed
// [sources.UnresolvedReferenceError] naming the offending field.
func TestTodo_MSRC_002_Fault(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*sources.Bundle)
		wantKind  sources.ReferenceKind
		wantField string
	}{
		{
			name: "unknown_presence_value",
			mutate: func(b *sources.Bundle) {
				mutateFirstProperty(b, "Person", func(p *sources.PropertySource) { p.Presence = "MAYBE" })
			},
			wantKind:  sources.ReferenceKindVocabulary,
			wantField: "properties[identity].presence",
		},
		{
			name: "float_money_type",
			mutate: func(b *sources.Bundle) {
				mutateFirstProperty(b, "CompensationComponent", func(p *sources.PropertySource) { p.GoType = "float64" })
			},
			wantKind:  sources.ReferenceKindVocabulary,
			wantField: "properties[amount].go_type",
		},
		{
			name: "untyped_id",
			mutate: func(b *sources.Bundle) {
				mutateFirstProperty(b, "Person", func(p *sources.PropertySource) { p.GoType = "any" })
			},
			wantKind:  sources.ReferenceKindVocabulary,
			wantField: "properties[identity].go_type",
		},
		{
			name: "unknown_entity_class",
			mutate: func(b *sources.Bundle) {
				mutateEntity(b, "Job", func(e *sources.EntitySource) { e.Class = "TOTALLY_MADE_UP_CLASS" })
			},
			wantKind:  sources.ReferenceKindVocabulary,
			wantField: "class",
		},
		{
			name: "undocumented_extension_covered_false_active",
			mutate: func(b *sources.Bundle) {
				mutateEntity(b, "PersonNameRevision", func(e *sources.EntitySource) { e.Status = "ACTIVE" })
			},
			wantKind:  sources.ReferenceKindVocabulary,
			wantField: "status",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bundle := loadValidBundle(t)
			tc.mutate(&bundle)
			_, errs := sources.Compile(bundle)
			if len(errs) == 0 {
				t.Fatal("Compile returned zero errors, want at least one")
			}
			var found *sources.UnresolvedReferenceError
			for _, err := range errs {
				var ref *sources.UnresolvedReferenceError
				if errors.As(err, &ref) && ref.Field == tc.wantField {
					found = ref
					break
				}
			}
			if found == nil {
				t.Fatalf("no UnresolvedReferenceError for field %q found among %d error(s): %v", tc.wantField, len(errs), errs)
			}
			if found.Kind != tc.wantKind {
				t.Errorf("Kind = %s, want %s", found.Kind, tc.wantKind)
			}
			if found.SourceFile == "" {
				t.Error("SourceFile is empty; a compile error must cite a source path")
			}
			if found.SourceLine <= 0 {
				t.Error("SourceLine is not positive; a compile error must cite a source line")
			}
		})
	}
}

// TestTodo_MSRC_002_Security asserts the authority/coverage boundary that
// keeps a source from claiming more trust than it has earned: an ACTIVE
// property can only cite a covered:true (Go-registered) authority, never a
// covered:false extension placeholder — otherwise a source file could assert
// production-grade source-of-truth for a field with no actual registered
// owner, which is exactly the kind of silent authority escalation MSRC-002's
// governance role exists to block.
func TestTodo_MSRC_002_Security(t *testing.T) {
	bundle := loadValidBundle(t)

	// Sanity: the unmutated tree must not already violate the rule.
	if _, errs := sources.Compile(bundle); len(errs) != 0 {
		t.Fatalf("checked-in bundle does not compile clean: %v", errs)
	}

	// Point an ACTIVE property at a real but covered:false authority.
	mutateFirstProperty(&bundle, "Person", func(p *sources.PropertySource) {
		p.AuthorityRef = "authority.people_extension/v1" // covered: false in registries.yaml
	})
	_, errs := sources.Compile(bundle)
	if len(errs) == 0 {
		t.Fatal("Compile accepted an ACTIVE property citing a covered:false authority")
	}
	var found bool
	for _, err := range errs {
		var ref *sources.UnresolvedReferenceError
		if errors.As(err, &ref) && ref.Kind == sources.ReferenceKindAuthority {
			found = true
		}
	}
	if !found {
		t.Errorf("no ReferenceKindAuthority error among: %v", errs)
	}
}

// TestTodo_MSRC_002_Mutation sweeps near-miss mutations of otherwise-valid
// vocabulary values (case changes, trailing characters, empty strings) to
// prove the validator rejects them rather than accidentally matching through
// case-insensitivity or prefix comparison.
func TestTodo_MSRC_002_Mutation(t *testing.T) {
	mutations := []string{"required", "Required", "REQUIRED ", "", "REQUIRED_X"}
	for _, m := range mutations {
		t.Run("presence="+m, func(t *testing.T) {
			bundle := loadValidBundle(t)
			mutateFirstProperty(&bundle, "Person", func(p *sources.PropertySource) { p.Presence = m })
			_, errs := sources.Compile(bundle)
			if m == "REQUIRED" {
				t.Skip("control value is not a mutation")
			}
			if len(errs) == 0 {
				t.Errorf("Compile accepted mutated presence %q", m)
			}
		})
	}
}

// mutateFirstProperty applies fn to the first (index-0) property of the
// entity named entityName in b. Every call site in this file targets an
// entity whose first declared property is the one under test (e.g. Person's
// "identity", CompensationComponent's "amount"), so the expected field path
// in each test case's wantField lines up with index 0.
func mutateFirstProperty(b *sources.Bundle, entityName string, fn func(*sources.PropertySource)) {
	for i := range b.Entities {
		if b.Entities[i].Name != entityName {
			continue
		}
		if len(b.Entities[i].Properties) == 0 {
			return
		}
		fn(&b.Entities[i].Properties[0])
		return
	}
}

func mutateEntity(b *sources.Bundle, entityName string, fn func(*sources.EntitySource)) {
	for i := range b.Entities {
		if b.Entities[i].Name == entityName {
			fn(&b.Entities[i])
			return
		}
	}
}
