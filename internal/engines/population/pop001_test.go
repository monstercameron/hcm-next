package population_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/population"
)

// TestTodo_POP_001 proves the RED and GREEN clauses of planning/todos.md
// POP-001: a definition missing an owner, an explicit subject type, scope,
// temporal policy, unknown-disclosure policy, count-disclosure policy or
// typed criteria is rejected, and a well-formed definition validates and
// publishes an immutable revision citing typed criteria and inputs.
func TestTodo_POP_001(t *testing.T) {
	t.Run("RED_missing_required_fields", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*population.Definition)
			want   error
		}{
			{"owner", func(d *population.Definition) { d.Owner = "" }, population.ErrDefinitionOwner},
			{"subject", func(d *population.Definition) { d.Subject = population.SubjectUnspecified }, population.ErrDefinitionSubject},
			{"invalid subject", func(d *population.Definition) { d.Subject = "NOT_A_KIND" }, population.ErrDefinitionSubject},
			{"tenant", func(d *population.Definition) { d.Scope.Tenant = "" }, population.ErrDefinitionScope},
			{"organization scope", func(d *population.Definition) { d.Scope.OrganizationScopeRef = "" }, population.ErrDefinitionScope},
			{"purpose", func(d *population.Definition) { d.Scope.Purpose = "" }, population.ErrDefinitionScope},
			{"temporal policy", func(d *population.Definition) { d.TemporalBasis = population.TemporalBasisUnspecified }, population.ErrDefinitionTemporalPolicy},
			{"unknown policy", func(d *population.Definition) { d.UnknownDisclosure = population.UnknownDisclosureUnspecified }, population.ErrDefinitionUnknownPolicy},
			{"count policy", func(d *population.Definition) { d.CountDisclosure = population.CountDisclosureUnspecified }, population.ErrDefinitionCountPolicy},
			{"empty criteria", func(d *population.Definition) { d.Criteria = population.Criteria{} }, population.ErrDefinitionCriteria},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := validDefinition()
				tc.mutate(&def)
				err := def.Validate()
				if err == nil {
					t.Fatalf("expected Validate to reject missing %s", tc.name)
				}
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want wrapping %v", err, tc.want)
				}
			})
		}
	})

	t.Run("RED_free_form_query_is_not_typed_criteria", func(t *testing.T) {
		def := validDefinition()
		// A criteria root with no kind at all is the typed-tree equivalent of a
		// caller trying to smuggle an untyped/free-form query past the shape
		// check: Validate must still reject it as empty/invalid criteria.
		def.Criteria = population.Criteria{Root: population.Predicate{Field: "grade", Values: []string{"P3"}}}
		if err := def.Validate(); err == nil {
			t.Fatal("expected a shapeless predicate to be rejected as untyped criteria")
		}
	})

	t.Run("GREEN_well_formed_definition_validates", func(t *testing.T) {
		def := validDefinition()
		if err := def.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if def.Canonical() == nil {
			t.Fatal("a valid definition must have a canonical encoding")
		}
	})

	t.Run("GREEN_revision_cites_typed_criteria_and_inputs", func(t *testing.T) {
		def := validDefinition()
		inputs := []population.InputRef{
			{Field: "grade", Kind: population.FieldTypeString},
			{Field: "active", Kind: population.FieldTypeString},
		}
		rev, err := population.NewRevision(def, "2026.1", inputs, mustInstant(t, 1000), mustKnownAt(t, 1000))
		if err != nil {
			t.Fatalf("NewRevision: %v", err)
		}
		if rev.Version != "2026.1" {
			t.Fatalf("version = %q, want 2026.1", rev.Version)
		}
		if len(rev.InputRefs()) != 2 {
			t.Fatalf("inputs = %d, want 2", len(rev.InputRefs()))
		}
		if rev.Canonical() == nil {
			t.Fatal("a valid revision must have a canonical encoding")
		}
	})

	t.Run("RED_revision_without_version_or_inputs", func(t *testing.T) {
		def := validDefinition()
		if _, err := population.NewRevision(def, "", []population.InputRef{{Field: "grade"}}, mustInstant(t, 1), mustKnownAt(t, 1)); err == nil {
			t.Fatal("expected a revision without a version to be rejected")
		}
		if _, err := population.NewRevision(def, "1", nil, mustInstant(t, 1), mustKnownAt(t, 1)); err == nil {
			t.Fatal("expected a revision without any typed input reference to be rejected")
		}
	})

	t.Run("immutability_returned_inputs_cannot_mutate_the_revision", func(t *testing.T) {
		def := validDefinition()
		inputs := []population.InputRef{{Field: "grade", Kind: population.FieldTypeString}}
		rev, err := population.NewRevision(def, "1", inputs, mustInstant(t, 1), mustKnownAt(t, 1))
		if err != nil {
			t.Fatalf("NewRevision: %v", err)
		}
		inputs[0].Field = "tampered"
		if rev.InputRefs()[0].Field == "tampered" {
			t.Fatal("mutating the caller's input slice mutated the stored revision")
		}
		got := rev.InputRefs()
		got[0].Field = "tampered-again"
		if rev.InputRefs()[0].Field == "tampered-again" {
			t.Fatal("mutating a returned InputRefs slice mutated the stored revision")
		}
	})
}

// TestTodo_POP_001_Property proves two structurally identical definitions
// (built independently, not by copying the same value) produce byte-identical
// canonical encodings, and that any single field difference changes it.
func TestTodo_POP_001_Property(t *testing.T) {
	a := validDefinition()
	b := validDefinition()
	if string(a.Canonical()) != string(b.Canonical()) {
		t.Fatal("two independently built, identical definitions produced different canonical bytes")
	}

	mutations := []func(*population.Definition){
		func(d *population.Definition) { d.Owner = "different-owner" },
		func(d *population.Definition) { d.Subject = population.SubjectPosition },
		func(d *population.Definition) { d.Scope.Purpose = "different-purpose" },
		func(d *population.Definition) { d.CountDisclosure = population.CountDisclosureSuppressed },
		func(d *population.Definition) {
			d.Criteria = population.Criteria{Root: fieldEquals("grade", "P4")}
		},
	}
	base := validDefinition().Canonical()
	for i, mutate := range mutations {
		mutated := validDefinition()
		mutate(&mutated)
		if string(mutated.Canonical()) == string(base) {
			t.Fatalf("mutation %d did not change the canonical encoding", i)
		}
	}
}

// TestTodo_POP_001_Golden pins the wire tokens every disclosure and temporal
// policy renders as, so a silent renumbering is caught by a test diff.
func TestTodo_POP_001_Golden(t *testing.T) {
	if got := population.UnknownDisclosureExcludeAndReport.String(); got != "EXCLUDE_AND_REPORT" {
		t.Fatalf("UnknownDisclosureExcludeAndReport = %q", got)
	}
	if got := population.UnknownDisclosureBlock.String(); got != "BLOCK" {
		t.Fatalf("UnknownDisclosureBlock = %q", got)
	}
	if got := population.CountDisclosureExact.String(); got != "EXACT" {
		t.Fatalf("CountDisclosureExact = %q", got)
	}
	if got := population.CountDisclosureBanded.String(); got != "BANDED" {
		t.Fatalf("CountDisclosureBanded = %q", got)
	}
	if got := population.CountDisclosureSuppressed.String(); got != "SUPPRESSED" {
		t.Fatalf("CountDisclosureSuppressed = %q", got)
	}
	if got := population.TemporalBasisAsOfCaller.String(); got != "AS_OF_CALLER" {
		t.Fatalf("TemporalBasisAsOfCaller = %q", got)
	}
	if got := population.SubjectWorker.String(); got != "WORKER" {
		t.Fatalf("SubjectWorker = %q", got)
	}
	if got := population.SubjectUnspecified.String(); got != "SUBJECT_UNSPECIFIED" {
		t.Fatalf("SubjectUnspecified = %q", got)
	}
}

// TestTodo_POP_001_Mutation proves every required field on Definition is load
// bearing: zeroing any one of them independently is refused, matching the
// exhaustive field list Validate checks.
func TestTodo_POP_001_Mutation(t *testing.T) {
	fields := map[string]func(*population.Definition){
		"id":                 func(d *population.Definition) { d.ID = "" },
		"owner":              func(d *population.Definition) { d.Owner = "" },
		"subject":            func(d *population.Definition) { d.Subject = "" },
		"tenant":             func(d *population.Definition) { d.Scope.Tenant = "" },
		"org scope":          func(d *population.Definition) { d.Scope.OrganizationScopeRef = "" },
		"purpose":            func(d *population.Definition) { d.Scope.Purpose = "" },
		"temporal basis":     func(d *population.Definition) { d.TemporalBasis = 0 },
		"unknown disclosure": func(d *population.Definition) { d.UnknownDisclosure = 0 },
		"count disclosure":   func(d *population.Definition) { d.CountDisclosure = 0 },
		"criteria":           func(d *population.Definition) { d.Criteria = population.Criteria{} },
	}
	for name, mutate := range fields {
		def := validDefinition()
		mutate(&def)
		if err := def.Validate(); err == nil {
			t.Fatalf("zeroing %s was accepted", name)
		}
		if def.Canonical() != nil {
			t.Fatalf("zeroing %s still produced a canonical encoding", name)
		}
	}
}
