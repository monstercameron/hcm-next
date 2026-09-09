package simassign_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestProposedEffectValidateNamesTheMissingDeclaration proves an incomplete
// effect is refused by name rather than reaching a plan half-built.
func TestProposedEffectValidateNamesTheMissingDeclaration(t *testing.T) {
	t.Parallel()
	complete, ok := simulate(t, newHarness(t), nil).Lookup(simassign.EffectAssignmentRevision)
	if !ok {
		t.Fatal("no assignment revision was proposed")
	}
	if err := complete.Validate(); err != nil {
		t.Fatalf("a fully derived effect failed validation: %v", err)
	}

	for _, tc := range []struct {
		name   string
		damage func(*simassign.ProposedEffect)
		want   string
	}{
		{"no effect id", func(e *simassign.ProposedEffect) { e.EffectID = "" }, "effect_id"},
		{"no participant", func(e *simassign.ProposedEffect) { e.Participant = "" }, "participant"},
		{"no compensation", func(e *simassign.ProposedEffect) { e.CompensationRef = "" }, "compensation_ref"},
		{"no observation", func(e *simassign.ProposedEffect) { e.ObservationRef = "" }, "observation_ref"},
		{"no idempotency key", func(e *simassign.ProposedEffect) { e.IdempotencyKey = "" }, "idempotency_key"},
		{"no authority decision", func(e *simassign.ProposedEffect) { e.AuthorityDecision = "" }, "authority_decision"},
		{"unknown reversibility", func(e *simassign.ProposedEffect) { e.Reversibility = "MAYBE" }, "reversibility"},
		{"no baseline", func(e *simassign.ProposedEffect) { e.ExpectedRevision = values.RevisionToken{} }, "baseline"},
		{"no derived-from", func(e *simassign.ProposedEffect) { e.DerivedFrom = nil }, "snapshot input"},
		{"no change", func(e *simassign.ProposedEffect) { e.Changes = nil }, "states no change"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			broken := complete
			tc.damage(&broken)
			err := broken.Validate()
			if !errors.Is(err, simassign.ErrEffectIncomplete) {
				t.Fatalf("Validate returned %v, want ErrEffectIncomplete", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate said %q, which does not name %q", err, tc.want)
			}
		})
	}
}

// TestPlannedProjectionsOmitUnchangedFields proves the kernel projections carry
// only what actually moves, and that a local effect never becomes an outbox
// record.
func TestPlannedProjectionsOmitUnchangedFields(t *testing.T) {
	t.Parallel()
	result := simulate(t, newHarness(t), nil)
	effect, ok := result.Lookup(simassign.EffectAssignmentRevision)
	if !ok {
		t.Fatal("no assignment revision was proposed")
	}

	changed := 0
	for _, change := range effect.Changes {
		if change.Changed {
			changed++
		}
	}
	if got := len(effect.PlannedWrites()); got != changed {
		t.Fatalf("projected %d planned writes for %d changed fields", got, changed)
	}
	if changed == len(effect.Changes) {
		t.Fatal("the fixture no longer exercises an unchanged field; the projection is untested")
	}

	planned := effect.PlannedEffect()
	if planned.EffectID != effect.EffectID || planned.Reversibility != string(effect.Reversibility) {
		t.Fatal("the planned-effect projection lost the effect's identity or class")
	}
	if planned.CompensationRef == "" || planned.ObservationRef == "" {
		t.Fatal("the planned-effect projection dropped the compensation or observation ref")
	}
	if !effect.Local {
		t.Fatal("the assignment revision is no longer local; the outbox assertion below is meaningless")
	}
	if got := len(result.OutboxEffects()); got != 0 {
		t.Fatalf("a local effect became %d outbox record(s)", got)
	}
	if got := len(result.Compensations()); got != 0 {
		t.Fatalf("a local effect declared %d plan compensation(s)", got)
	}
	if got, want := len(result.Participants()), len(result.Effects); got != want {
		t.Fatalf("projected %d participants for %d effects", got, want)
	}
	for _, participant := range result.Participants() {
		if !participant.Local {
			t.Fatalf("participant %s is outside the local commit boundary", participant.ParticipantID)
		}
	}
}

// TestRefusalForInputMapsEveryNonDisclosure proves a denial, a known absence
// and an unestablished presence never collapse into one cause, and that a
// refusal carries no value.
func TestRefusalForInputMapsEveryNonDisclosure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		availability promosnapshot.Availability
		want         simassign.Reason
	}{
		{promosnapshot.AvailabilityWithheld, simassign.ReasonInputWithheld},
		{promosnapshot.AvailabilityAbsent, simassign.ReasonInputAbsent},
		{promosnapshot.AvailabilityUnknown, simassign.ReasonInputUnknown},
	} {
		t.Run(string(tc.availability), func(t *testing.T) {
			t.Parallel()
			in := promosnapshot.Input{
				Name:          promosnapshot.InputCurrentPlacement,
				Availability:  tc.availability,
				Reason:        "policy:test",
				CanonicalText: "assignment.job_code=SECRET",
			}
			refusal := simassign.RefusalForInput(simassign.EffectAssignmentRevision, in)
			if refusal.Reason != tc.want {
				t.Fatalf("availability %s produced reason %s, want %s", tc.availability, refusal.Reason, tc.want)
			}
			if strings.Contains(refusal.Error(), "SECRET") {
				t.Fatalf("the refusal quotes the value it could not disclose: %s", refusal.Error())
			}
			if !errors.Is(refusal, simassign.ErrRefused) {
				t.Fatal("the refusal does not match ErrRefused")
			}
			if len(refusal.Canonical()) == 0 {
				t.Fatal("the refusal has no canonical encoding")
			}
		})
	}
}

// TestDerivedFromSortsAndDeduplicates proves the citation list is canonical, so
// two effects citing the same inputs in different orders digest identically.
func TestDerivedFromSortsAndDeduplicates(t *testing.T) {
	t.Parallel()
	got := simassign.DerivedFrom("b", "a", "b", "c", "a")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("DerivedFrom returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DerivedFrom returned %v, want %v", got, want)
		}
	}
}
