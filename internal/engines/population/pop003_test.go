package population_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
)

// TestTodo_POP_003 proves the RED and GREEN clauses of planning/todos.md
// POP-003: a fact the source claims to know only after the requested
// knowledge boundary cannot silently change a historical resolution, calling
// Resolve twice at the same boundary is idempotent (interval-boundary
// duplicates never accumulate), a stale source watermark is recorded rather
// than treated as a true absence, and the result records source watermarks
// and time context.
func TestTodo_POP_003(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())
	w1 := worker(1)

	t.Run("RED_future_known_correction_does_not_change_historical_membership", func(t *testing.T) {
		futureKnownAt := mustKnownAt(t, 2_000_000) // after the requested boundary
		reader := newFakeReader().
			withSubject(w1).
			withFact(w1, "grade", population.Fact{Presence: valueFactPresence("P3"), KnownAt: futureKnownAt}).
			withFact(w1, "active", valueFact("true", knownAt)).
			withWatermark(mustInstant(t, 1_000_000))

		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(result.Members) != 1 || result.Members[0].Outcome != population.OutcomeUnknown {
			t.Fatalf("members = %+v, want exactly one UNKNOWN member", result.Members)
		}
		foundFutureKnowledge := false
		for _, ob := range result.Members[0].Obligations {
			if ob.Reason == population.ObligationFutureKnowledge {
				foundFutureKnowledge = true
			}
		}
		if !foundFutureKnowledge {
			t.Fatalf("obligations = %+v, want ObligationFutureKnowledge", result.Members[0].Obligations)
		}
	})

	t.Run("RED_stale_watermark_is_not_treated_as_true_absence", func(t *testing.T) {
		reader := newFakeReader().
			withSubject(w1).
			withFact(w1, "grade", valueFact("P3", knownAt)).
			// active is never asserted, and the watermark is before knownAt: the
			// source has not caught up, so this must not be read as "confirmed
			// absent and therefore excluded".
			withWatermark(mustInstant(t, 500_000))

		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Members[0].Outcome != population.OutcomeUnknown {
			t.Fatalf("outcome = %s, want UNKNOWN", result.Members[0].Outcome)
		}
		if result.Members[0].Obligations[len(result.Members[0].Obligations)-1].Reason != population.ObligationStaleWatermark {
			t.Fatalf("obligations = %+v, want a trailing ObligationStaleWatermark", result.Members[0].Obligations)
		}
	})

	t.Run("GREEN_well_formed_resolution_includes_matching_subject", func(t *testing.T) {
		reader := newFakeReader().
			withSubject(w1).
			withFact(w1, "grade", valueFact("P3", knownAt)).
			withFact(w1, "active", valueFact("true", knownAt)).
			withWatermark(asOf)

		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Completeness != population.CompletenessComplete {
			t.Fatalf("completeness = %s, want COMPLETE", result.Completeness)
		}
		included := result.Included()
		if len(included) != 1 || included[0] != w1 {
			t.Fatalf("included = %v, want [%s]", included, w1)
		}
		if wm, ok := result.SourceWatermarks[population.SubjectWorker]; !ok || wm != asOf {
			t.Fatalf("source watermark not recorded: %+v", result.SourceWatermarks)
		}
		if result.AsOf != asOf || result.KnownAt != knownAt {
			t.Fatal("result did not record the requested time context")
		}
	})

	t.Run("GREEN_excluded_subject_when_criteria_definitely_fails", func(t *testing.T) {
		reader := newFakeReader().
			withSubject(w1).
			withFact(w1, "grade", valueFact("P4", knownAt)).
			withFact(w1, "active", valueFact("true", knownAt)).
			withWatermark(asOf)

		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Members[0].Outcome != population.OutcomeExcluded {
			t.Fatalf("outcome = %s, want EXCLUDED", result.Members[0].Outcome)
		}
	})

	t.Run("RED_unresolved_identity_is_unknown_not_excluded", func(t *testing.T) {
		reader := newFakeReader().withSubject(invalidRef()).withWatermark(asOf)
		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Members[0].Outcome != population.OutcomeUnknown {
			t.Fatalf("outcome = %s, want UNKNOWN", result.Members[0].Outcome)
		}
		if result.Members[0].Obligations[0].Reason != population.ObligationIdentityUnresolved {
			t.Fatalf("obligation = %v, want ObligationIdentityUnresolved", result.Members[0].Obligations)
		}
		if result.Completeness != population.CompletenessPartial {
			t.Fatalf("completeness = %s, want PARTIAL", result.Completeness)
		}
	})

	t.Run("RED_source_unavailable_is_unknown", func(t *testing.T) {
		reader := newFakeReader().withSubject(w1).withWatermark(asOf).
			withReadErr(w1, "grade", errors.New("boom"))
		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Members[0].Outcome != population.OutcomeUnknown {
			t.Fatalf("outcome = %s, want UNKNOWN", result.Members[0].Outcome)
		}
	})
}

// TestTodo_POP_003_Property proves resolution is deterministic and
// idempotent: two Resolve calls with byte-identical inputs (same reader
// state, same plan, same time context, including an as-of instant that sits
// exactly on a fact's known-at boundary) produce identical members and
// completeness every time.
func TestTodo_POP_003_Property(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000) // exactly on the fact's own known-at
	plan := mustPlan(t, validDefinition())
	w1 := worker(1)

	reader := newFakeReader().
		withSubject(w1).
		withFact(w1, "grade", valueFact("P3", knownAt)).
		withFact(w1, "active", valueFact("true", knownAt)).
		withWatermark(asOf)

	first, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	second, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
	if err != nil {
		t.Fatalf("Resolve (repeat): %v", err)
	}
	if first.Completeness != second.Completeness {
		t.Fatal("completeness differed across identical resolutions")
	}
	if len(first.Members) != len(second.Members) || first.Members[0].Outcome != second.Members[0].Outcome {
		t.Fatal("members differed across identical resolutions")
	}
	// A fact known exactly at the boundary (neither before nor after) must be
	// trusted, not treated as a future-known correction: the boundary is
	// inclusive.
	if first.Members[0].Outcome != population.OutcomeIncluded {
		t.Fatalf("outcome at the exact known-at boundary = %s, want INCLUDED", first.Members[0].Outcome)
	}
}

// TestTodo_POP_003_Mutation proves the three time-integrity guards inside
// Resolve are independently load bearing: removing any one of them changes
// which of these three fixed scenarios is (mis)classified.
func TestTodo_POP_003_Mutation(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())
	w1 := worker(1)

	t.Run("a future-known fact must not be silently trusted", func(t *testing.T) {
		reader := newFakeReader().withSubject(w1).
			withFact(w1, "grade", valueFact("P3", mustKnownAt(t, 9_000_000))).
			withFact(w1, "active", valueFact("true", knownAt)).
			withWatermark(asOf)
		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Members[0].Outcome == population.OutcomeIncluded {
			t.Fatal("a future-known correction was trusted and changed the historical resolution")
		}
	})

	t.Run("an unvalidated subject reference must not silently resolve", func(t *testing.T) {
		reader := newFakeReader().withSubject(invalidRef()).withWatermark(asOf)
		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Members[0].Outcome != population.OutcomeUnknown {
			t.Fatal("an unresolved identity produced a definite outcome instead of UNKNOWN")
		}
	})

	t.Run("a stale watermark must not be indistinguishable from a caught-up source", func(t *testing.T) {
		reader := newFakeReader().withSubject(w1).withFact(w1, "grade", valueFact("P3", knownAt)).
			withWatermark(mustInstant(t, 1)) // far in the past
		result, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		found := false
		for _, ob := range result.Members[0].Obligations {
			if ob.Reason == population.ObligationStaleWatermark {
				found = true
			}
		}
		if !found {
			t.Fatal("a stale-watermark source did not produce ObligationStaleWatermark")
		}
	})
}

// TestTodo_POP_008 proves the RED and GREEN clauses of planning/todos.md
// POP-008: an unavailable source, a stale watermark, a denied fact and an
// unresolved identity all produce OutcomeUnknown, never OutcomeExcluded and
// never CompletenessComplete, and the typed obligation propagates to the
// caller.
func TestTodo_POP_008(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())

	cases := []struct {
		name   string
		reader func() *fakeReader
		wantOb population.ObligationReason
	}{
		{
			"unavailable source",
			func() *fakeReader {
				return newFakeReader().withSubject(worker(1)).withWatermark(asOf).
					withFact(worker(1), "grade", population.Fact{Presence: unavailableFact()})
			},
			population.ObligationSourceUnavailable,
		},
		{
			"redacted fact",
			func() *fakeReader {
				return newFakeReader().withSubject(worker(1)).withWatermark(asOf).
					withFact(worker(1), "grade", population.Fact{Presence: redactedFact()})
			},
			population.ObligationFactRedacted,
		},
		{
			"stale watermark",
			func() *fakeReader {
				return newFakeReader().withSubject(worker(1)).withWatermark(mustInstant(t, 1))
			},
			population.ObligationStaleWatermark,
		},
		{
			"unresolved identity",
			func() *fakeReader {
				return newFakeReader().withSubject(invalidRef()).withWatermark(asOf)
			},
			population.ObligationIdentityUnresolved,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := population.Resolve(ctx, tc.reader(), population.SubjectWorker, plan, asOf, knownAt)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			m := result.Members[0]
			if m.Outcome == population.OutcomeExcluded {
				t.Fatal("an undetermined subject was reported as EXCLUDED")
			}
			if m.Outcome != population.OutcomeUnknown {
				t.Fatalf("outcome = %s, want UNKNOWN", m.Outcome)
			}
			if result.Completeness == population.CompletenessComplete {
				t.Fatal("a resolution containing an UNKNOWN member reported COMPLETE")
			}
			found := false
			for _, ob := range m.Obligations {
				if ob.Reason == tc.wantOb {
					found = true
				}
			}
			if !found {
				t.Fatalf("obligations = %+v, want %s", m.Obligations, tc.wantOb)
			}
		})
	}
}

// TestTodo_POP_008_Property proves that whenever any member's outcome is
// UNKNOWN, the overall completeness is never COMPLETE, and whenever every
// member is definite, completeness is never PARTIAL: the two are always
// consistent with each other.
func TestTodo_POP_008_Property(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())

	allKnown := newFakeReader().
		withSubject(worker(1)).
		withFact(worker(1), "grade", valueFact("P3", knownAt)).
		withFact(worker(1), "active", valueFact("true", knownAt)).
		withWatermark(asOf)
	result, err := population.Resolve(ctx, allKnown, population.SubjectWorker, plan, asOf, knownAt)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	hasUnknown := false
	for _, m := range result.Members {
		if m.Outcome == population.OutcomeUnknown {
			hasUnknown = true
		}
	}
	if hasUnknown && result.Completeness == population.CompletenessComplete {
		t.Fatal("an UNKNOWN member coexisted with COMPLETE completeness")
	}
	if !hasUnknown && result.Completeness != population.CompletenessComplete {
		t.Fatal("no UNKNOWN member existed but completeness was not COMPLETE")
	}

	withUnknown := newFakeReader().
		withSubject(worker(1)).
		withWatermark(asOf) // grade/active never asserted -> UNKNOWN
	result2, err := population.Resolve(ctx, withUnknown, population.SubjectWorker, plan, asOf, knownAt)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result2.Completeness != population.CompletenessPartial {
		t.Fatalf("completeness = %s, want PARTIAL", result2.Completeness)
	}
}
