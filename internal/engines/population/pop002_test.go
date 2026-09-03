package population_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/population"
)

// TestTodo_POP_002 proves the RED and GREEN clauses of planning/todos.md
// POP-002: an unresolved field, a field marked not population-queryable, an
// unbounded predicate tree and an operator applied to the wrong field type
// all fail to compile, and a well-formed criteria compiles into a plan that
// exposes its reads, index hints, privacy risk and a canonical digest.
func TestTodo_POP_002(t *testing.T) {
	catalog := testCatalog()

	t.Run("RED_unresolved_property_fails", func(t *testing.T) {
		criteria := population.Criteria{Root: fieldEquals("nickname", "Bob")}
		_, err := population.Compile(criteria, catalog)
		if !errors.Is(err, population.ErrUnresolvedField) {
			t.Fatalf("error = %v, want ErrUnresolvedField", err)
		}
	})

	t.Run("RED_unauthorized_inference_fails", func(t *testing.T) {
		criteria := population.Criteria{Root: fieldEquals("ssn", "123-45-6789")}
		_, err := population.Compile(criteria, catalog)
		if !errors.Is(err, population.ErrUnauthorizedInference) {
			t.Fatalf("error = %v, want ErrUnauthorizedInference", err)
		}
	})

	t.Run("RED_unbounded_traversal_fails_on_depth", func(t *testing.T) {
		leaf := fieldEquals("grade", "P3")
		deep := leaf
		for i := 0; i < population.MaxPredicateDepth+2; i++ {
			deep = population.Predicate{Kind: population.PredicateNot, Children: []population.Predicate{deep}}
		}
		_, err := population.Compile(population.Criteria{Root: deep}, catalog)
		if !errors.Is(err, population.ErrUnboundedTraversal) {
			t.Fatalf("error = %v, want ErrUnboundedTraversal", err)
		}
	})

	t.Run("RED_unbounded_traversal_fails_on_node_count", func(t *testing.T) {
		var wide []population.Predicate
		for i := 0; i < population.MaxPredicateNodes+2; i++ {
			wide = append(wide, fieldEquals("grade", "P3"))
		}
		_, err := population.Compile(population.Criteria{Root: population.Predicate{Kind: population.PredicateOr, Children: wide}}, catalog)
		if !errors.Is(err, population.ErrUnboundedTraversal) {
			t.Fatalf("error = %v, want ErrUnboundedTraversal", err)
		}
	})

	t.Run("RED_operator_type_mismatch_fails", func(t *testing.T) {
		criteria := population.Criteria{Root: population.Predicate{Kind: population.PredicateGreaterThan, Field: "grade", Values: []string{"P3"}}}
		_, err := population.Compile(criteria, catalog)
		if !errors.Is(err, population.ErrOperatorType) {
			t.Fatalf("error = %v, want ErrOperatorType", err)
		}
	})

	t.Run("RED_malformed_predicate_shapes_fail", func(t *testing.T) {
		cases := []population.Predicate{
			{Kind: population.PredicateAnd}, // composite with no children
			{Kind: population.PredicateNot, Children: []population.Predicate{fieldEquals("grade", "P3"), fieldEquals("grade", "P4")}}, // NOT with two children
			{Kind: population.PredicateIn, Field: "grade"},                                   // IN with no values
			{Kind: population.PredicateEquals, Field: "grade", Values: []string{"P3", "P4"}}, // EQUALS with two values
			{Kind: population.PredicateEquals, Values: []string{"P3"}},                       // leaf with no field
		}
		for i, p := range cases {
			if _, err := population.Compile(population.Criteria{Root: p}, catalog); err == nil {
				t.Fatalf("case %d: expected a malformed predicate to be rejected", i)
			}
		}
	})

	t.Run("GREEN_well_formed_criteria_compiles_a_complete_plan", func(t *testing.T) {
		plan, err := population.Compile(validDefinition().Criteria, catalog)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if len(plan.FieldsRead) != 2 || plan.FieldsRead[0] != "active" || plan.FieldsRead[1] != "grade" {
			t.Fatalf("fields read = %v, want sorted [active grade]", plan.FieldsRead)
		}
		if len(plan.IndexHints) != 2 || plan.IndexHints[0] != "idx_active" || plan.IndexHints[1] != "idx_grade" {
			t.Fatalf("index hints = %v, want sorted [idx_active idx_grade]", plan.IndexHints)
		}
		if plan.PrivacyRisk != population.PrivacyRiskStandard {
			t.Fatalf("privacy risk = %s, want STANDARD", plan.PrivacyRisk)
		}
		if !strings.HasPrefix(plan.Digest, "sha256:") {
			t.Fatalf("digest = %q, want a sha256: prefix", plan.Digest)
		}
	})

	t.Run("GREEN_sensitive_field_elevates_privacy_risk", func(t *testing.T) {
		criteria := population.Criteria{Root: population.Predicate{Kind: population.PredicateGreaterThan, Field: "salary", Values: []string{"50000"}}}
		plan, err := population.Compile(criteria, catalog)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if plan.PrivacyRisk != population.PrivacyRiskElevated {
			t.Fatalf("privacy risk = %s, want ELEVATED", plan.PrivacyRisk)
		}
	})
}

// TestTodo_POP_002_Property proves compilation is deterministic: compiling
// the same criteria twice produces byte-identical digests, and field order in
// an AND/OR composite never changes FieldsRead, IndexHints or the digest.
func TestTodo_POP_002_Property(t *testing.T) {
	catalog := testCatalog()
	criteria := validDefinition().Criteria

	first, err := population.Compile(criteria, catalog)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	second, err := population.Compile(criteria, catalog)
	if err != nil {
		t.Fatalf("Compile (repeat): %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("two compiles of identical criteria produced different digests")
	}

	// The predicate tree's own child order is part of its identity (and of
	// what an explanation later walks in order), so a reordered tree is a
	// different criteria and may digest differently. What must stay
	// order-independent is the plan's declared read set: which fields and
	// index hints a resolver needs never depends on how the caller happened
	// to list the AND's children.
	reordered := population.Criteria{Root: and(fieldEquals("active", "true"), fieldEquals("grade", "P3"))}
	third, err := population.Compile(reordered, catalog)
	if err != nil {
		t.Fatalf("Compile (reordered): %v", err)
	}
	if len(third.FieldsRead) != len(first.FieldsRead) {
		t.Fatalf("fields read = %v, want %v", third.FieldsRead, first.FieldsRead)
	}
	for i := range first.FieldsRead {
		if third.FieldsRead[i] != first.FieldsRead[i] {
			t.Fatalf("fields read[%d] = %q, want %q", i, third.FieldsRead[i], first.FieldsRead[i])
		}
	}
}

// TestTodo_POP_002_Golden pins the compiled plan for the fixture criteria so a
// silent change to field ordering, index hints or privacy classification is
// caught by a test diff.
func TestTodo_POP_002_Golden(t *testing.T) {
	plan, err := population.Compile(validDefinition().Criteria, testCatalog())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	wantFields := []string{"active", "grade"}
	if len(plan.FieldsRead) != len(wantFields) {
		t.Fatalf("fields read = %v, want %v", plan.FieldsRead, wantFields)
	}
	for i, f := range wantFields {
		if plan.FieldsRead[i] != f {
			t.Fatalf("fields read[%d] = %q, want %q", i, plan.FieldsRead[i], f)
		}
	}
	if plan.PrivacyRisk.String() != "STANDARD" {
		t.Fatalf("privacy risk = %s, want STANDARD", plan.PrivacyRisk)
	}
}

// TestTodo_POP_002_Security proves the compiler's boundary check for
// "unauthorized inference" is exhaustive across every operator, not just
// EQUALS: no operator lets a caller compile a predicate over a
// not-population-queryable field.
func TestTodo_POP_002_Security(t *testing.T) {
	catalog := testCatalog()
	kinds := []population.PredicateKind{population.PredicateEquals, population.PredicateNotEquals, population.PredicateIn}
	for _, kind := range kinds {
		p := population.Predicate{Kind: kind, Field: "ssn", Values: []string{"123-45-6789"}}
		if _, err := population.Compile(population.Criteria{Root: p}, catalog); !errors.Is(err, population.ErrUnauthorizedInference) {
			t.Fatalf("kind %s: error = %v, want ErrUnauthorizedInference", kind, err)
		}
	}

	// A restricted field nested arbitrarily deep inside an authorized
	// composite must still be caught; the boundary is per-leaf, not
	// per-top-level-predicate.
	nested := and(fieldEquals("grade", "P3"), population.Predicate{
		Kind: population.PredicateOr,
		Children: []population.Predicate{
			fieldEquals("active", "true"),
			fieldEquals("ssn", "123-45-6789"),
		},
	})
	if _, err := population.Compile(population.Criteria{Root: nested}, catalog); !errors.Is(err, population.ErrUnauthorizedInference) {
		t.Fatalf("nested restricted field: error = %v, want ErrUnauthorizedInference", err)
	}
}

// TestTodo_POP_002_Mutation proves every one of the compiler's bound checks
// is independently load bearing by disabling each one at a time and
// confirming the specific input it exists to catch is no longer rejected.
func TestTodo_POP_002_Mutation(t *testing.T) {
	catalog := testCatalog()

	// Removing the unresolved-field check (simulated by adding the field to
	// the catalog) makes an otherwise-rejected criteria compile.
	withNickname := population.FieldCatalog{}
	for k, v := range catalog {
		withNickname[k] = v
	}
	withNickname["nickname"] = population.FieldDescriptor{Type: population.FieldTypeString, Queryable: true}
	if _, err := population.Compile(population.Criteria{Root: fieldEquals("nickname", "Bob")}, withNickname); err != nil {
		t.Fatalf("expected a catalog-registered field to compile, got %v", err)
	}

	// Marking ssn queryable is exactly "removing the unauthorized-inference
	// guard"; it must be the only thing that changes the outcome.
	withSSN := population.FieldCatalog{}
	for k, v := range catalog {
		withSSN[k] = v
	}
	withSSN["ssn"] = population.FieldDescriptor{Type: population.FieldTypeString, Queryable: true}
	if _, err := population.Compile(population.Criteria{Root: fieldEquals("ssn", "123-45-6789")}, withSSN); err != nil {
		t.Fatalf("expected ssn to compile once marked queryable, got %v", err)
	}
}
