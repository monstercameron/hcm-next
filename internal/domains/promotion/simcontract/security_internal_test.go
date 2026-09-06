package simcontract

import "testing"

// TestSideEffectStatusIsUnexported is the white-box half of
// TestTodo_PROMO_004_Security. It runs inside package simcontract, the one
// place the unexported status field is reachable at all, and proves that even
// from here -- past the compiler's own package-boundary enforcement -- a
// [SideEffect] whose status is not exactly [EffectSimulatedNotExecuted]
// refuses to validate. The security property is therefore not merely "no
// caller can reach the field"; it is "even a value carrying an illegal status
// is caught by Validate", which is what protects the contract if this field
// is ever accidentally exported or gains a setter later.
func TestSideEffectStatusIsUnexported(t *testing.T) {
	base := SideEffect{
		EffectID:        "effect-1",
		Kind:            "test.kind",
		Participant:     "test.participant",
		DestinationRef:  "test.destination",
		Reversibility:   "REVERSIBLE",
		CompensationRef: "test.compensation",
		ObservationRef:  "test.observation",
	}

	t.Run("zero_value_status_is_rejected", func(t *testing.T) {
		if err := base.Validate(); err == nil {
			t.Fatal("a side effect built without NewSideEffect (status left at its zero value) validated")
		}
	})

	t.Run("executed_status_is_rejected", func(t *testing.T) {
		tampered := base
		tampered.status = "EXECUTED"
		if err := tampered.Validate(); err == nil {
			t.Fatal("a side effect whose status was set to EXECUTED validated")
		}
	})

	t.Run("only_simulated_not_executed_is_accepted", func(t *testing.T) {
		legal := base
		legal.status = EffectSimulatedNotExecuted
		if err := legal.Validate(); err != nil {
			t.Fatalf("a side effect stamped %s must validate: %v", EffectSimulatedNotExecuted, err)
		}
	})
}
