package conflict_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

func mustInstant(t *testing.T, year, month, day int) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC))
}

// candidateOn builds a valid Candidate for a fixed field/resource, varying
// only the proposal identity, state and declared relationships.
func candidateOn(t *testing.T, resource values.ResourceKey, field conflict.FieldPath, revID string, state conflict.ProposalState) conflict.Candidate {
	t.Helper()
	return conflict.Candidate{
		ProposalRevisionID: revID,
		State:              state,
		RecordedAt:         mustInstant(t, 2026, 9, 1),
		Footprint: conflict.WriteFootprint{
			Resource:         resource,
			Field:            field,
			Interval:         mustOpenInterval(t, 2026, 10, 1),
			Operation:        conflict.OperationUpdate,
			ExpectedRevision: mustSequenceRevision(t, "people.employment.9001", 42),
			Authority:        conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local_master/v1"},
		},
	}
}

// TestTodo_CONFLICT_002 is the PRIMARY test for classifying concurrent
// proposal conflicts.
//
// RED: pending/approved/future/executed overlapping writes are ignored or
// nondeterministically classified.
//
// GREEN: fixtures return exactly COMPATIBLE_MERGE, ORDERED_DEPENDENCY,
// SUPERSESSION, or HARD_CONFLICT with evidence/owner.
func TestTodo_CONFLICT_002(t *testing.T) {
	resource := mustResourceKey(t, "employment", "9001", "primary")
	const field = conflict.FieldPath("employment.assignment.position_ref")

	t.Run("RED: no state is ever silently ignored", func(t *testing.T) {
		states := []conflict.ProposalState{
			conflict.ProposalPending, conflict.ProposalApproved,
			conflict.ProposalFutureDated, conflict.ProposalExecuted,
		}
		for _, state := range states {
			t.Run(string(state), func(t *testing.T) {
				a := candidateOn(t, resource, field, "rev:a", state)
				b := candidateOn(t, resource, field, "rev:b", conflict.ProposalPending)
				got, err := conflict.ClassifyConflict(a, b, nil)
				if err != nil {
					t.Fatalf("classify: %v", err)
				}
				if got.Decision == conflict.DecisionUnspecified {
					t.Fatalf("state %s was classified as UNSPECIFIED (ignored)", state)
				}
			})
		}
	})

	t.Run("RED: an unspecified proposal state is rejected, not silently ignored", func(t *testing.T) {
		a := candidateOn(t, resource, field, "rev:a", "")
		b := candidateOn(t, resource, field, "rev:b", conflict.ProposalPending)
		if _, err := conflict.ClassifyConflict(a, b, nil); !errors.Is(err, conflict.ErrInvalidCandidate) {
			t.Fatalf("err=%v, want ErrInvalidCandidate", err)
		}
	})

	t.Run("RED: classification is deterministic regardless of argument order", func(t *testing.T) {
		a := candidateOn(t, resource, field, "rev:a", conflict.ProposalPending)
		b := candidateOn(t, resource, field, "rev:b", conflict.ProposalApproved)
		forward, err := conflict.ClassifyConflict(a, b, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		backward, err := conflict.ClassifyConflict(b, a, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if forward != backward {
			t.Fatalf("ClassifyConflict(a,b)=%+v != ClassifyConflict(b,a)=%+v", forward, backward)
		}
	})

	t.Run("RED: non-overlapping candidates are never classified", func(t *testing.T) {
		a := candidateOn(t, resource, field, "rev:a", conflict.ProposalPending)
		b := candidateOn(t, resource, "employment.assignment.grade", "rev:b", conflict.ProposalPending)
		// "position_ref" and "grade" are siblings, not ancestors: they must
		// not overlap.
		if _, err := conflict.ClassifyConflict(a, b, nil); !errors.Is(err, conflict.ErrNoOverlap) {
			t.Fatalf("err=%v, want ErrNoOverlap", err)
		}
	})

	t.Run("GREEN: HARD_CONFLICT with no merge proof, dependency or supersession", func(t *testing.T) {
		a := candidateOn(t, resource, field, "rev:a", conflict.ProposalPending)
		b := candidateOn(t, resource, field, "rev:b", conflict.ProposalApproved)
		got, err := conflict.ClassifyConflict(a, b, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if got.Decision != conflict.DecisionHardConflict {
			t.Fatalf("decision = %s, want HARD_CONFLICT", got.Decision)
		}
		if got.EvidenceRef == "" || got.Explanation == "" {
			t.Fatalf("HARD_CONFLICT carries no evidence/explanation: %+v", got)
		}
	})

	t.Run("GREEN: SUPERSESSION with an owner", func(t *testing.T) {
		a := candidateOn(t, resource, field, "rev:a", conflict.ProposalExecuted)
		b := candidateOn(t, resource, field, "rev:b", conflict.ProposalPending)
		b.SupersedesRevisionID = "rev:a"
		got, err := conflict.ClassifyConflict(a, b, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if got.Decision != conflict.DecisionSupersession {
			t.Fatalf("decision = %s, want SUPERSESSION", got.Decision)
		}
		if got.OwnerProposalRevisionID != "rev:b" {
			t.Fatalf("owner = %q, want the superseding revision rev:b", got.OwnerProposalRevisionID)
		}
		if got.EvidenceRef == "" {
			t.Fatalf("SUPERSESSION carries no evidence")
		}
		// Symmetric regardless of call order.
		got2, err := conflict.ClassifyConflict(b, a, nil)
		if err != nil {
			t.Fatalf("classify (reversed): %v", err)
		}
		if got2 != got {
			t.Fatalf("SUPERSESSION classification is order-dependent: %+v vs %+v", got, got2)
		}
	})

	t.Run("GREEN: ORDERED_DEPENDENCY with the prerequisite as owner", func(t *testing.T) {
		a := candidateOn(t, resource, field, "rev:a", conflict.ProposalApproved)
		b := candidateOn(t, resource, field, "rev:b", conflict.ProposalPending)
		b.DependsOnRevisionID = "rev:a"
		got, err := conflict.ClassifyConflict(a, b, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if got.Decision != conflict.DecisionOrderedDependency {
			t.Fatalf("decision = %s, want ORDERED_DEPENDENCY", got.Decision)
		}
		if got.OwnerProposalRevisionID != "rev:a" {
			t.Fatalf("owner = %q, want the prerequisite revision rev:a", got.OwnerProposalRevisionID)
		}
	})

	t.Run("GREEN: COMPATIBLE_MERGE only when a versioned rule proves it", func(t *testing.T) {
		a := candidateOn(t, resource, field, "rev:a", conflict.ProposalPending)
		b := candidateOn(t, resource, field, "rev:b", conflict.ProposalPending)

		// With no rule, the pair is a HARD_CONFLICT: the registry never
		// merges arbitrary writes on its own.
		got, err := conflict.ClassifyConflict(a, b, nil)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if got.Decision != conflict.DecisionHardConflict {
			t.Fatalf("with no merge rule, decision = %s, want HARD_CONFLICT", got.Decision)
		}

		rules := []conflict.MergeRule{{
			RuleRef: "identical_value_merge", Version: "v1",
			Proves: func(x, y conflict.Candidate) (string, bool) {
				return "proof:identical_value_merge/v1", true
			},
		}}
		got, err = conflict.ClassifyConflict(a, b, rules)
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if got.Decision != conflict.DecisionCompatibleMerge {
			t.Fatalf("with a proving rule, decision = %s, want COMPATIBLE_MERGE", got.Decision)
		}
		if got.EvidenceRef != "proof:identical_value_merge/v1" {
			t.Fatalf("COMPATIBLE_MERGE evidence = %q, want the rule's proof ref", got.EvidenceRef)
		}
	})

	t.Run("GREEN: ClassifyAll covers every overlapping pair and skips disjoint ones", func(t *testing.T) {
		other := mustResourceKey(t, "employment", "9002", "primary")
		candidates := []conflict.Candidate{
			candidateOn(t, resource, field, "rev:a", conflict.ProposalPending),
			candidateOn(t, resource, field, "rev:b", conflict.ProposalApproved),
			candidateOn(t, other, field, "rev:c", conflict.ProposalPending), // different resource: no overlap
		}
		classifications, err := conflict.ClassifyAll(candidates, nil)
		if err != nil {
			t.Fatalf("classify all: %v", err)
		}
		if len(classifications) != 1 {
			t.Fatalf("classified %d pair(s), want exactly the one overlapping pair", len(classifications))
		}
		if classifications[0].Decision != conflict.DecisionHardConflict {
			t.Fatalf("decision = %s, want HARD_CONFLICT", classifications[0].Decision)
		}
	})
}

// TestTodo_CONFLICT_002_Race classifies the same overlapping pair
// concurrently from many goroutines and requires one stable classification:
// nondeterministic classification under concurrent evaluation is exactly the
// CONFLICT-002 RED case for two racing preflights.
func TestTodo_CONFLICT_002_Race(t *testing.T) {
	resource := mustResourceKey(t, "employment", "9001", "primary")
	const field = conflict.FieldPath("employment.assignment.position_ref")
	a := candidateOn(t, resource, field, "rev:a", conflict.ProposalPending)
	b := candidateOn(t, resource, field, "rev:b", conflict.ProposalApproved)
	rules := []conflict.MergeRule{
		{RuleRef: "z_rule", Version: "v1", Proves: func(x, y conflict.Candidate) (string, bool) { return "", false }},
		{RuleRef: "a_rule", Version: "v1", Proves: func(x, y conflict.Candidate) (string, bool) { return "", false }},
	}

	const n = 32
	results := make([]conflict.Classification, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Alternate argument order across goroutines: the result must
			// not depend on it.
			var (
				got conflict.Classification
				err error
			)
			if i%2 == 0 {
				got, err = conflict.ClassifyConflict(a, b, rules)
			} else {
				got, err = conflict.ClassifyConflict(b, a, rules)
			}
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			results[i] = got
		}(i)
	}
	wg.Wait()

	for i := 1; i < n; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d produced a different classification: %+v vs %+v", i, results[i], results[0])
		}
	}
}
