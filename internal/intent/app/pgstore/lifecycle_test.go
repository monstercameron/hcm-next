package pgstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// TestLifecycleMutatorContract asserts the compile-time port satisfaction,
// exactly as the sibling TestOutcomeBinderContract does for OutcomeBinder.
func TestLifecycleMutatorContract(t *testing.T) {
	var _ app.LifecycleMutator = (*Store)(nil)
}

// TestMutateLifecycleValidatesBeforeTouchingTheDatabase proves
// MutateLifecycle rejects a structurally invalid mutation before it ever
// opens a transaction: every case below runs with a nil DB, so a case that
// reached s.db.Begin would panic on the nil pointer rather than return the
// typed validation error this test asserts on. That is deliberate -- it is
// what proves these checks precede the compare-and-swap rather than relying
// on the database to reject bad input.
func TestMutateLifecycleValidatesBeforeTouchingTheDatabase(t *testing.T) {
	s := &Store{}
	validLifecycle := lifecycle.Dimensions{
		Request: lifecycle.RequestCancelled, Execution: lifecycle.ExecutionNotPlanned,
		Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyNotApplicable,
		Obligation: lifecycle.ObligationNotApplicable,
	}

	cases := []struct {
		name string
		m    app.LifecycleMutation
	}{
		{"missing tenant", app.LifecycleMutation{
			IntentID: "11111111-1111-1111-1111-111111111111", ExpectedInstanceVersion: 1, Lifecycle: validLifecycle,
		}},
		{"zero expected instance version", app.LifecycleMutation{
			Tenant: "acme-corp", IntentID: "11111111-1111-1111-1111-111111111111", Lifecycle: validLifecycle,
		}},
		{"invalid lifecycle dimensions", app.LifecycleMutation{
			Tenant: "acme-corp", IntentID: "11111111-1111-1111-1111-111111111111", ExpectedInstanceVersion: 1,
			Lifecycle: lifecycle.Dimensions{Request: lifecycle.RequestState(200)},
		}},
		{"non-uuid intent id", app.LifecycleMutation{
			Tenant: "acme-corp", IntentID: "not-a-uuid", ExpectedInstanceVersion: 1, Lifecycle: validLifecycle,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.MutateLifecycle(context.Background(), tc.m); err == nil {
				t.Fatalf("MutateLifecycle(%s) succeeded with a nil database, want a validation refusal", tc.name)
			}
		})
	}
}
