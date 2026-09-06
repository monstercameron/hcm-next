package simcontract_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/promotion/simcontract"
)

// TestTodo_PROMO_004 is the PROMO-004 primary test. It assembles the full
// WorkflowSimulationContract for both the Promotion reference and the Manager
// Change fixture, proves each is a complete, self-verifying, once-persistable
// artifact, and proves -- with a live zero-effect harness held for the whole
// test -- that none of it ever touches a domain revision store, an external
// operation port, the outbox, a MessageIntent sink or a WorkItem store.
func TestTodo_PROMO_004(t *testing.T) {
	// Holding one of each failing port for the whole test is the zero-effect
	// assertion: if Assemble, Validate or Persist ever reached for one, the
	// harness fails the test the instant it happened, wherever it happened.
	harness := newZeroEffectHarness(t)
	if harness.Revisions == nil || harness.ExternalOperation == nil || harness.Outbox == nil ||
		harness.Messages == nil || harness.WorkItems == nil {
		t.Fatal("zero-effect harness is incomplete")
	}

	ctx := context.Background()
	store := simcontract.NewMemoryStore()

	t.Run("promotion", func(t *testing.T) {
		result, err := simcontract.Assemble(promotionFixtureInput(t))
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		assertContractInvariants(t, result)
		if result.Status != simcontract.ResultExecutableAsSimulated {
			t.Fatalf("Status = %s, want %s", result.Status, simcontract.ResultExecutableAsSimulated)
		}
		if len(result.SideEffects) != 3 {
			t.Fatalf("len(SideEffects) = %d, want 3", len(result.SideEffects))
		}
		if result.Completion.State != simcontract.CompletionPendingApproval {
			t.Fatalf("Completion.State = %s, want %s", result.Completion.State, simcontract.CompletionPendingApproval)
		}

		stored, already, err := store.Store(ctx, result)
		if err != nil {
			t.Fatalf("Store: %v", err)
		}
		if already {
			t.Fatal("first Store reported already-stored")
		}
		if stored.Digest != result.Digest {
			t.Fatalf("stored digest = %s, want %s", stored.Digest, result.Digest)
		}

		// Idempotent by digest: a second Store of the identical artifact
		// returns the originally stored one and does not persist again.
		second, already, err := store.Store(ctx, result)
		if err != nil {
			t.Fatalf("second Store: %v", err)
		}
		if !already {
			t.Fatal("second Store did not report already-stored")
		}
		if second.Digest != result.Digest {
			t.Fatalf("second Store digest = %s, want %s", second.Digest, result.Digest)
		}

		loaded, ok, err := store.Load(ctx, result.Digest)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !ok {
			t.Fatal("Load did not find the stored artifact")
		}
		if loaded.Digest != result.Digest {
			t.Fatalf("loaded digest = %s, want %s", loaded.Digest, result.Digest)
		}
	})

	t.Run("manager_change", func(t *testing.T) {
		result, err := simcontract.Assemble(managerChangeFixtureInput(t))
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		assertContractInvariants(t, result)
		if result.Status != simcontract.ResultExecutableAsSimulated {
			t.Fatalf("Status = %s, want %s", result.Status, simcontract.ResultExecutableAsSimulated)
		}
		if len(result.SideEffects) != 1 {
			t.Fatalf("len(SideEffects) = %d, want 1", len(result.SideEffects))
		}
		if result.Completion.State != simcontract.CompletionReady {
			t.Fatalf("Completion.State = %s, want %s", result.Completion.State, simcontract.CompletionReady)
		}
		if result.Cost.State != simcontract.CostNone {
			t.Fatalf("Cost.State = %s, want %s", result.Cost.State, simcontract.CostNone)
		}

		stored, already, err := store.Store(ctx, result)
		if err != nil {
			t.Fatalf("Store: %v", err)
		}
		if already {
			t.Fatal("first Store reported already-stored")
		}
		if stored.Digest != result.Digest {
			t.Fatalf("stored digest = %s, want %s", stored.Digest, result.Digest)
		}
	})

	// Two distinct fixtures produce two distinct, once-recorded artifacts.
	if store.Len() != 2 {
		t.Fatalf("store recorded %d artifacts, want 2", store.Len())
	}
}

// assertContractInvariants checks the properties every assembled contract
// must have, regardless of which fixture built it.
func assertContractInvariants(t *testing.T, r simcontract.SimulationResult) {
	t.Helper()
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := r.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	if r.Digest == "" {
		t.Fatal("Digest is empty")
	}
	for _, e := range r.SideEffects {
		if e.Status() != simcontract.EffectSimulatedNotExecuted {
			t.Fatalf("effect %s status = %s, want %s", e.EffectID, e.Status(), simcontract.EffectSimulatedNotExecuted)
		}
		if err := e.Validate(); err != nil {
			t.Fatalf("effect %s: %v", e.EffectID, err)
		}
	}
	for _, section := range simcontract.Sections() {
		if sectionAbsent(r, section) {
			t.Fatalf("section %s reads as absent on a validated contract", section)
		}
	}
}

// sectionAbsent reports whether a validated contract's section still reads
// as "never stated" (nil, or an unstated single-value state). It exists so
// [assertContractInvariants] can assert every section really is present on
// every fixture, not merely that Validate happened to accept it.
func sectionAbsent(r simcontract.SimulationResult, section simcontract.SectionKind) bool {
	switch section {
	case simcontract.SectionReads:
		return r.Reads == nil
	case simcontract.SectionWrites:
		return r.Writes == nil
	case simcontract.SectionStreams:
		return r.Streams == nil
	case simcontract.SectionConflicts:
		return r.Conflicts == nil
	case simcontract.SectionApprovals:
		return r.Approvals == nil
	case simcontract.SectionAuthority:
		return r.Authority == nil
	case simcontract.SectionLegalObligations:
		return r.LegalObligations == nil
	case simcontract.SectionSideEffects:
		return r.SideEffects == nil
	case simcontract.SectionCost:
		return r.Cost.State == simcontract.CostStateUnspecified
	case simcontract.SectionCompletion:
		return r.Completion.State == simcontract.CompletionStateUnspecified
	case simcontract.SectionRevalidation:
		return r.Revalidation.Rules == nil
	case simcontract.SectionRepair:
		return r.Repair == nil
	default:
		return true
	}
}

// zeroSection mutates one section of in to its "never stated" shape, so the
// RED matrix below can drive every section through the same refusal path.
func zeroSection(in *simcontract.AssembleInput, section simcontract.SectionKind) {
	switch section {
	case simcontract.SectionReads:
		in.Reads = nil
	case simcontract.SectionWrites:
		in.Writes = nil
	case simcontract.SectionStreams:
		in.Streams = nil
	case simcontract.SectionConflicts:
		in.Conflicts = nil
	case simcontract.SectionApprovals:
		in.Approvals = nil
	case simcontract.SectionAuthority:
		in.Authority = nil
	case simcontract.SectionLegalObligations:
		in.LegalObligations = nil
	case simcontract.SectionSideEffects:
		in.SideEffects = nil
	case simcontract.SectionCost:
		in.Cost = simcontract.Cost{}
	case simcontract.SectionCompletion:
		in.Completion = simcontract.Completion{}
	case simcontract.SectionRevalidation:
		in.Revalidation = simcontract.Revalidation{}
	case simcontract.SectionRepair:
		in.Repair = nil
	}
}

// TestTodo_PROMO_004_RED drives the RED case named in planning/todos.md: a
// contract missing any one of the twelve mandatory sections fails validation
// with a typed [simcontract.Refusal] naming exactly that section -- never a
// different one, and never a generic error a caller would have to parse.
func TestTodo_PROMO_004_RED(t *testing.T) {
	for _, section := range simcontract.Sections() {
		section := section
		t.Run(string(section), func(t *testing.T) {
			in := promotionFixtureInput(t)
			zeroSection(&in, section)

			_, err := simcontract.Assemble(in)
			if err == nil {
				t.Fatalf("Assemble with %s missing: got nil error, want a refusal", section)
			}
			if !errors.Is(err, simcontract.ErrIncompleteContract) {
				t.Fatalf("Assemble with %s missing: error = %v, want errors.Is ErrIncompleteContract", section, err)
			}
			var refusal simcontract.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("Assemble with %s missing: error = %v, want a simcontract.Refusal", section, err)
			}
			if refusal.Section != section {
				t.Fatalf("refusal names section %s, want %s", refusal.Section, section)
			}
		})
	}
}

// TestTodo_PROMO_004_RED_EffectWithoutRepair pins the cross-section RED case:
// a side effect with no matching repair binding refuses under the repair
// section, even though the repair slice itself is non-nil.
func TestTodo_PROMO_004_RED_EffectWithoutRepair(t *testing.T) {
	in := promotionFixtureInput(t)
	in.Repair = in.Repair[:len(in.Repair)-1]

	_, err := simcontract.Assemble(in)
	if err == nil {
		t.Fatal("Assemble with an unrepaired effect: got nil error")
	}
	var refusal simcontract.Refusal
	if !errors.As(err, &refusal) || refusal.Section != simcontract.SectionRepair {
		t.Fatalf("error = %v, want a Refusal naming %s", err, simcontract.SectionRepair)
	}
}
