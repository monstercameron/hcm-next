package population_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/population"
)

// TestTodo_POP_007 proves the RED and GREEN clauses of planning/todos.md
// POP-007: the explanation identifies each criterion's matched/failed/unknown
// status and the applied policy versions, a restricted (non-disclosable)
// field's name is withheld from the explanation rather than shown, and the
// explanation for one subject never references or computes anything about
// another subject.
func TestTodo_POP_007(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())
	w1 := worker(1)
	versions := mustPolicyVersions()

	t.Run("GREEN_matched_criteria_are_named_when_disclosable", func(t *testing.T) {
		reader := newFakeReader().withSubject(w1).
			withFact(w1, "grade", valueFact("P3", knownAt)).
			withFact(w1, "active", valueFact("true", knownAt)).
			withWatermark(asOf)
		disclosable := map[string]bool{"grade": true, "active": true}

		explanation, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, disclosable, versions)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if explanation.Outcome != population.OutcomeIncluded {
			t.Fatalf("outcome = %s, want INCLUDED", explanation.Outcome)
		}
		if len(explanation.Criteria) != 2 {
			t.Fatalf("criteria = %+v, want 2 leaves", explanation.Criteria)
		}
		for _, c := range explanation.Criteria {
			if c.Redacted {
				t.Fatalf("criterion %+v was redacted despite full disclosure", c)
			}
			if c.Status != population.CriterionMatched {
				t.Fatalf("criterion %+v status = %s, want MATCHED", c, c.Status)
			}
			if c.Field == "" {
				t.Fatal("a disclosable criterion did not name its field")
			}
		}
		if explanation.Versions != versions {
			t.Fatalf("versions = %+v, want %+v", explanation.Versions, versions)
		}
	})

	t.Run("RED_restricted_field_name_is_withheld", func(t *testing.T) {
		reader := newFakeReader().withSubject(w1).
			withFact(w1, "grade", valueFact("P3", knownAt)).
			withFact(w1, "active", valueFact("true", knownAt)).
			withWatermark(asOf)
		// "active" is not in the disclosable set: its status still contributes,
		// but its field name must not appear.
		disclosable := map[string]bool{"grade": true}

		explanation, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, disclosable, versions)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		foundRedacted := false
		for _, c := range explanation.Criteria {
			if c.Field == "active" {
				t.Fatal("a non-disclosable field name leaked into the explanation")
			}
			if c.Redacted {
				foundRedacted = true
				if c.Field != "" {
					t.Fatalf("a redacted criterion still carried a field name: %+v", c)
				}
			}
		}
		if !foundRedacted {
			t.Fatal("expected at least one redacted criterion")
		}
	})

	t.Run("GREEN_unknown_criterion_carries_its_obligation", func(t *testing.T) {
		reader := newFakeReader().withSubject(w1).
			withFact(w1, "grade", valueFact("P3", knownAt)).
			withWatermark(asOf) // active never asserted
		disclosable := map[string]bool{"grade": true, "active": true}

		explanation, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, disclosable, versions)
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if explanation.Outcome != population.OutcomeUnknown {
			t.Fatalf("outcome = %s, want UNKNOWN", explanation.Outcome)
		}
		found := false
		for _, c := range explanation.Criteria {
			if c.Field == "active" {
				if c.Status != population.CriterionUnknown {
					t.Fatalf("active status = %s, want UNKNOWN", c.Status)
				}
				if c.Obligation == population.ObligationUnspecified {
					t.Fatal("an unknown criterion did not carry an obligation")
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected the active leaf to appear in the explanation")
		}
	})

	t.Run("RED_incomplete_policy_versions_fail_closed", func(t *testing.T) {
		reader := newFakeReader().withSubject(w1).withWatermark(asOf)
		_, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, map[string]bool{}, population.PolicyVersions{})
		if err == nil {
			t.Fatal("expected Explain without policy versions to fail")
		}
	})

	t.Run("GREEN_explanation_is_scoped_to_exactly_one_subject", func(t *testing.T) {
		w2 := worker(2)
		reader := newFakeReader().
			withSubject(w1).withFact(w1, "grade", valueFact("P3", knownAt)).withFact(w1, "active", valueFact("true", knownAt)).
			withSubject(w2).withFact(w2, "grade", valueFact("P9", knownAt)).withFact(w2, "active", valueFact("false", knownAt)).
			withWatermark(asOf)
		disclosable := map[string]bool{"grade": true, "active": true}

		e1, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, disclosable, versions)
		if err != nil {
			t.Fatalf("Explain(w1): %v", err)
		}
		if e1.Subject != w1 {
			t.Fatalf("explanation subject = %s, want %s", e1.Subject, w1)
		}
		if e1.Outcome != population.OutcomeIncluded {
			t.Fatalf("w1 outcome = %s, want INCLUDED (w2's failing facts must not leak in)", e1.Outcome)
		}
	})
}

// TestTodo_POP_007_Property proves Explain is deterministic and side-effect
// free: two calls with identical inputs produce byte-identical digests, and
// the number of criterion entries always equals the number of leaves in the
// compiled plan's criteria tree regardless of disclosure.
func TestTodo_POP_007_Property(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())
	w1 := worker(1)
	versions := mustPolicyVersions()
	reader := newFakeReader().withSubject(w1).
		withFact(w1, "grade", valueFact("P3", knownAt)).
		withFact(w1, "active", valueFact("true", knownAt)).
		withWatermark(asOf)

	first, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, map[string]bool{"grade": true, "active": true}, versions)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	second, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, map[string]bool{"grade": true, "active": true}, versions)
	if err != nil {
		t.Fatalf("Explain (repeat): %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("two explanations of identical inputs produced different digests")
	}

	redacted, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, map[string]bool{}, versions)
	if err != nil {
		t.Fatalf("Explain (fully redacted): %v", err)
	}
	if len(redacted.Criteria) != len(first.Criteria) {
		t.Fatalf("criteria count changed with disclosure: %d vs %d", len(redacted.Criteria), len(first.Criteria))
	}
	if redacted.Digest == first.Digest {
		t.Fatal("redacting field names did not change the digest")
	}
}

// TestTodo_POP_007_Mutation proves the disclosure gate is load bearing per
// field: authorizing only one of the two leaves redacts exactly the other,
// never both and never neither.
func TestTodo_POP_007_Mutation(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())
	w1 := worker(1)
	versions := mustPolicyVersions()
	reader := newFakeReader().withSubject(w1).
		withFact(w1, "grade", valueFact("P3", knownAt)).
		withFact(w1, "active", valueFact("true", knownAt)).
		withWatermark(asOf)

	explanation, err := population.Explain(ctx, reader, plan, w1, asOf, knownAt, map[string]bool{"grade": true}, versions)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	redactedCount, namedCount := 0, 0
	for _, c := range explanation.Criteria {
		if c.Redacted {
			redactedCount++
		} else {
			namedCount++
			if c.Field != "grade" {
				t.Fatalf("the only disclosed field was %q, want grade", c.Field)
			}
		}
	}
	if redactedCount != 1 || namedCount != 1 {
		t.Fatalf("redacted=%d named=%d, want exactly one of each", redactedCount, namedCount)
	}
}
