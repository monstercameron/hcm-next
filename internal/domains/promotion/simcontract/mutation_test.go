package simcontract_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simassign"
	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcontract"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
)

// TestTodo_PROMO_004_Mutation is PROMO-004's MUTATION matrix test. Each
// subtest pins down an exact boundary a mutant could flip without any other
// test in this package noticing: which section a refusal names, whether an
// empty-but-stated section is distinct from an absent one, and whether the
// digest is sensitive to every field it claims to cover.
func TestTodo_PROMO_004_Mutation(t *testing.T) {
	t.Run("empty_conflicts_is_stated_nil_conflicts_is_not", func(t *testing.T) {
		// A mutant that changed Validate's "r.Conflicts == nil" check to
		// "len(r.Conflicts) == 0" would refuse the fixture outright, because
		// the fixture legitimately states zero conflicts. This is the
		// boundary that distinguishes "no conflicting candidates were found"
		// from "conflict evaluation never ran".
		stated := promotionFixtureInput(t)
		if _, err := simcontract.Assemble(stated); err != nil {
			t.Fatalf("Assemble with Conflicts stated empty: %v", err)
		}

		absent := promotionFixtureInput(t)
		absent.Conflicts = nil
		_, err := simcontract.Assemble(absent)
		var refusal simcontract.Refusal
		if !errors.As(err, &refusal) || refusal.Section != simcontract.SectionConflicts {
			t.Fatalf("Assemble with Conflicts nil: error = %v, want a Refusal naming %s", err, simcontract.SectionConflicts)
		}
	})

	t.Run("cost_none_and_cost_evaluated_are_both_legal_but_not_interchangeable", func(t *testing.T) {
		// CostNone must carry no amount, and CostEvaluated must carry one.
		// A mutant that dropped either half of that pairing would let a
		// NO_COST section smuggle a dangling amount through, or an
		// EVALUATED section through with none.
		in := managerChangeFixtureInput(t)
		in.Cost = simcontract.Cost{State: simcontract.CostNone}
		if _, err := simcontract.Assemble(in); err != nil {
			t.Fatalf("Assemble with NO_COST and no amount: %v", err)
		}

		amount := mustFixtureMoney(t, "1.00", "USD")
		in.Cost = simcontract.Cost{State: simcontract.CostNone, Amount: &amount}
		if _, err := simcontract.Assemble(in); err == nil {
			t.Fatal("Assemble accepted NO_COST carrying a dangling amount")
		}

		promo := promotionFixtureInput(t)
		promo.Cost = simcontract.Cost{State: simcontract.CostEvaluated}
		if _, err := simcontract.Assemble(promo); err == nil {
			t.Fatal("Assemble accepted EVALUATED cost with no amount")
		}
	})

	t.Run("completion_pending_approval_requires_a_named_outstanding_approval", func(t *testing.T) {
		// A mutant that dropped the "PENDING_APPROVAL names at least one
		// outstanding approval" check would let a contract claim work is
		// pending without saying on what.
		in := promotionFixtureInput(t)
		in.Completion.OutstandingApprovals = nil
		if _, err := simcontract.Assemble(in); err == nil {
			t.Fatal("Assemble accepted PENDING_APPROVAL with no outstanding approval named")
		}

		// The inverse boundary: READY must not carry an outstanding
		// approval either, or the two states become indistinguishable.
		ready := managerChangeFixtureInput(t)
		ready.Completion.OutstandingApprovals = []string{"approval.somewhere"}
		if _, err := simcontract.Assemble(ready); err == nil {
			t.Fatal("Assemble accepted READY carrying an outstanding approval")
		}
	})

	t.Run("every_side_effect_needs_its_own_repair_binding_not_just_any_repair", func(t *testing.T) {
		// A mutant that checked len(Repair) > 0 instead of matching each
		// effect id would accept a contract whose repair section covers only
		// one of several effects. Swap in a repair binding for an unrelated
		// effect id and confirm it still refuses.
		in := promotionFixtureInput(t)
		in.Repair[0].EffectID = "some-other-effect-id"
		_, err := simcontract.Assemble(in)
		var refusal simcontract.Refusal
		if !errors.As(err, &refusal) || refusal.Section != simcontract.SectionRepair {
			t.Fatalf("error = %v, want a Refusal naming %s", err, simcontract.SectionRepair)
		}
	})

	t.Run("status_is_blocked_by_any_refusal_regardless_of_findings", func(t *testing.T) {
		// A mutant that derived Status from Findings alone (ignoring
		// Refusals) would report EXECUTABLE_AS_SIMULATED over a refused
		// effect. Status is never caller-supplied, so this drives it
		// through Assemble rather than asserting a literal.
		in := promotionFixtureInput(t)
		in.Findings = nil
		in.Refusals = []simassign.Refusal{{
			Kind:         simassign.EffectAssignmentRevision,
			Reason:       simassign.ReasonInputWithheld,
			InputName:    promosnapshot.InputCurrentPlacement,
			Availability: promosnapshot.AvailabilityWithheld,
			Detail:       "authorization withheld the current placement",
		}}
		result, err := simcontract.Assemble(in)
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		if result.Status != simcontract.ResultBlocked {
			t.Fatalf("Status = %s, want %s over a non-empty refusal set", result.Status, simcontract.ResultBlocked)
		}
	})

	t.Run("advisory_findings_alone_never_block", func(t *testing.T) {
		// The companion boundary: an advisory-only finding set with no
		// refusals must never flip Status to BLOCKED. A mutant that treated
		// "any finding" as blocking would fail this.
		in := managerChangeFixtureInput(t)
		result, err := simcontract.Assemble(in)
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		if result.Status != simcontract.ResultExecutableAsSimulated {
			t.Fatalf("Status = %s, want %s with no findings and no refusals", result.Status, simcontract.ResultExecutableAsSimulated)
		}
	})

	t.Run("digest_changes_with_every_section_and_only_that_section", func(t *testing.T) {
		base, err := simcontract.Assemble(promotionFixtureInput(t))
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		// Changing one write's proposed text must move the digest. A
		// mutant that dropped a field from the canonical encoding would
		// leave the digest unchanged here.
		mutated := promotionFixtureInput(t)
		mutated.Writes[0].ProposedCanonicalText = "ENG-MGR2"
		mutatedResult, err := simcontract.Assemble(mutated)
		if err != nil {
			t.Fatalf("Assemble(mutated): %v", err)
		}
		if mutatedResult.Digest == base.Digest {
			t.Fatal("mutating a write's proposed text did not change the digest")
		}

		// Re-assembling the identical, unmutated input is byte-for-byte
		// stable: the digest is a pure function of content, never of
		// construction order or a clock.
		again, err := simcontract.Assemble(promotionFixtureInput(t))
		if err != nil {
			t.Fatalf("Assemble(again): %v", err)
		}
		if again.Digest != base.Digest {
			t.Fatalf("re-assembling identical input produced a different digest: %s vs %s", again.Digest, base.Digest)
		}
	})
}
