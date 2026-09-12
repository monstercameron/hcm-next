package intent_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func supersedeDefinition() intent.Definition {
	return intent.Definition{
		Ref:    intent.Ref{TypeID: "hcmnext.test.supersede_fixture", Version: 1},
		Family: intent.FamilyChangeRequest,
	}
}

func supersedeFixtureInstance(request lifecycle.RequestState) intent.Instance {
	def := supersedeDefinition()
	digestID := "33333333-3333-3333-3333-333333333333"
	return intent.Instance{
		IntentID:   digestID,
		Definition: def.Ref,
		Tenant:     "acme-corp",
		Purpose:    "hcm_operations",
		Lifecycle: lifecycle.Dimensions{
			Request:     request,
			Execution:   lifecycle.ExecutionNotPlanned,
			Business:    lifecycle.BusinessNotStarted,
			Consistency: lifecycle.ConsistencyNotApplicable,
			Obligation:  lifecycle.ObligationNotApplicable,
		},
		ProposalRevisions: []intent.ProposalRevision{{
			ProposalRevisionID: "revision-1",
			IntentID:           digestID,
			Revision:           1,
		}},
		InstanceVersion: 4,
	}
}

func fixedClockAt(at time.Time) intent.Clock {
	return func() values.Instant { return values.NewInstant(at) }
}

// TestTodo_EP_INTENT_003_Supersession is the PRIMARY unit test for
// [intent.SupersedeOriginal]: the original's own record is byte-identical
// afterwards except for its request state moving to SUPERSEDED -- a state
// transition, not a rewrite -- and every prior proposal revision on the
// original survives.
func TestTodo_EP_INTENT_003_Supersession(t *testing.T) {
	def := supersedeDefinition()
	recordedAt := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	t.Run("supersedes cleanly from SIMULATED, SUBMITTED and APPROVED", func(t *testing.T) {
		for _, request := range []lifecycle.RequestState{
			lifecycle.RequestSimulated, lifecycle.RequestSubmitted, lifecycle.RequestApproved,
		} {
			original := supersedeFixtureInstance(request)
			before := original
			rec, err := intent.SupersedeOriginal(&original, def, "successor-1", "reason:replaced", fixedClockAt(recordedAt))
			if err != nil {
				t.Fatalf("SupersedeOriginal(%s): %v", request, err)
			}
			if rec.SupersededIntentID != before.IntentID || rec.SupersedingIntentID != "successor-1" || rec.ReasonRef != "reason:replaced" {
				t.Fatalf("record = %+v", rec)
			}
			if original.Lifecycle.Request != lifecycle.RequestSuperseded {
				t.Fatalf("RequestState = %s, want SUPERSEDED", original.Lifecycle.Request)
			}
			if original.InstanceVersion != before.InstanceVersion+1 {
				t.Fatalf("InstanceVersion = %d, want %d", original.InstanceVersion, before.InstanceVersion+1)
			}
			// Every other field -- the whole point of this todo's clause --
			// survives untouched. Diff by clearing exactly the fields the
			// transition is documented to move and asserting deep equality
			// on everything else.
			after := original
			after.Lifecycle.Request = before.Lifecycle.Request
			after.InstanceVersion = before.InstanceVersion
			after.RecordedAt = before.RecordedAt
			after.LastTransitionAt = before.LastTransitionAt
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("SupersedeOriginal mutated more than lifecycle/version/timestamps:\nbefore=%+v\nafter =%+v", before, after)
			}
			if len(original.ProposalRevisions) != 1 || original.ProposalRevisions[0].ProposalRevisionID != "revision-1" {
				t.Fatalf("proposal revisions did not survive: %+v", original.ProposalRevisions)
			}
		}
	})

	t.Run("every terminal request state refuses a second supersede", func(t *testing.T) {
		for _, terminal := range []lifecycle.RequestState{
			lifecycle.RequestCancelled, lifecycle.RequestSuperseded,
			lifecycle.RequestRejected, lifecycle.RequestWithdrawn, lifecycle.RequestClosed,
		} {
			inst := supersedeFixtureInstance(terminal)
			_, err := intent.SupersedeOriginal(&inst, def, "successor-1", "reason:replaced", fixedClockAt(recordedAt))
			if !errors.Is(err, intent.ErrSupersedeAlreadyTerminal) {
				t.Fatalf("SupersedeOriginal(%s) = %v, want ErrSupersedeAlreadyTerminal", terminal, err)
			}
		}
	})

	t.Run("an intent may not supersede itself", func(t *testing.T) {
		inst := supersedeFixtureInstance(lifecycle.RequestSimulated)
		_, err := intent.SupersedeOriginal(&inst, def, inst.IntentID, "reason:replaced", fixedClockAt(recordedAt))
		if !errors.Is(err, intent.ErrSupersedeSelf) {
			t.Fatalf("err = %v, want ErrSupersedeSelf", err)
		}
	})

	t.Run("a zero value never means permissive", func(t *testing.T) {
		inst := supersedeFixtureInstance(lifecycle.RequestSimulated)
		if _, err := intent.SupersedeOriginal(&inst, def, "", "reason:replaced", fixedClockAt(recordedAt)); err == nil {
			t.Fatal("SupersedeOriginal with no successor id succeeded, want a refusal")
		}
		inst2 := supersedeFixtureInstance(lifecycle.RequestSimulated)
		if _, err := intent.SupersedeOriginal(&inst2, def, "successor-1", "", fixedClockAt(recordedAt)); err == nil {
			t.Fatal("SupersedeOriginal with no reason_ref succeeded, want a refusal")
		}
	})

	t.Run("DRAFT and PREFLIGHTED have no declared supersede edge", func(t *testing.T) {
		for _, request := range []lifecycle.RequestState{lifecycle.RequestDraft, lifecycle.RequestPreflighted} {
			inst := supersedeFixtureInstance(request)
			if _, err := intent.SupersedeOriginal(&inst, def, "successor-1", "reason:replaced", fixedClockAt(recordedAt)); err == nil {
				t.Fatalf("SupersedeOriginal(%s) succeeded, want a refusal (no declared edge)", request)
			}
		}
	})

	t.Run("nil instance and definition mismatch fail closed", func(t *testing.T) {
		if _, err := intent.SupersedeOriginal(nil, def, "successor-1", "reason:replaced", fixedClockAt(recordedAt)); err == nil {
			t.Fatal("SupersedeOriginal(nil) succeeded, want a refusal")
		}
		inst := supersedeFixtureInstance(lifecycle.RequestSimulated)
		other := def
		other.Ref = intent.Ref{TypeID: "hcmnext.test.other", Version: 1}
		if _, err := intent.SupersedeOriginal(&inst, other, "successor-1", "reason:replaced", fixedClockAt(recordedAt)); err == nil {
			t.Fatal("SupersedeOriginal against a mismatched definition succeeded, want a refusal")
		}
	})
}
