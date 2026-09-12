package intent_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// submitDefinition is a minimal, self-contained ChangeRequest definition with
// no AllowedTransitions narrowing, so [intent.Submit] is exercised against
// the raw kernel lifecycle profile ([lifecycle.KernelProfiles]) rather than
// against a production definition's own narrower edge set (that composition
// is exercised separately, at the application layer, against the checked-in
// production catalog).
func submitDefinition() intent.Definition {
	return intent.Definition{
		Ref:    intent.Ref{TypeID: "hcmnext.test.submit_fixture", Version: 1},
		Family: intent.FamilyChangeRequest,
	}
}

func submitFixtureInstance(request lifecycle.RequestState) intent.Instance {
	def := submitDefinition()
	return intent.Instance{
		IntentID:   "11111111-1111-1111-1111-111111111111",
		Definition: def.Ref,
		Lifecycle: lifecycle.Dimensions{
			Request:     request,
			Execution:   lifecycle.ExecutionNotPlanned,
			Business:    lifecycle.BusinessNotStarted,
			Consistency: lifecycle.ConsistencyNotApplicable,
			Obligation:  lifecycle.ObligationNotApplicable,
		},
		InstanceVersion: 3,
	}
}

// TestTodo_EP_INTENT_003_Submission is the PRIMARY unit test for
// [intent.Submit]: it binds the presented proposal revision and starts
// exactly once.
//
// RED: a terminal intent is revived, an intent with no declared path to
// SUBMITTED at all is accepted, or a second submit of an already-submitted
// intent starts a second run (observed here as a second dimensional
// transition and a second version bump).
//
// GREEN: DRAFT, PREFLIGHTED and SIMULATED each reach SUBMITTED through their
// own declared hops (P1A's SimulateIntent persists nothing, so Submit is the
// one governed write that both persists "this was preflighted and
// simulated" and binds the resulting proposal); an intent already at
// SUBMITTED is returned byte-for-byte unchanged.
func TestTodo_EP_INTENT_003_Submission(t *testing.T) {
	def := submitDefinition()
	recordedAt := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

	t.Run("SIMULATED submits exactly once", func(t *testing.T) {
		inst := submitFixtureInstance(lifecycle.RequestSimulated)
		beforeVersion := inst.InstanceVersion

		if err := intent.Submit(&inst, def, intent.SubmitRequest{
			ProposalRevisionID: "revision-1",
			RecordedAt:         recordedAt,
		}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if inst.Lifecycle.Request != lifecycle.RequestSubmitted {
			t.Fatalf("RequestState = %s, want SUBMITTED", inst.Lifecycle.Request)
		}
		if inst.InstanceVersion != beforeVersion+1 {
			t.Fatalf("InstanceVersion = %d, want %d", inst.InstanceVersion, beforeVersion+1)
		}
		if inst.Lifecycle.Execution != lifecycle.ExecutionNotPlanned {
			t.Fatalf("ExecutionState moved to %s, want unchanged NOT_PLANNED", inst.Lifecycle.Execution)
		}

		// A second submit of the now-SUBMITTED intent must not start a
		// second run: no further transition, no further version bump.
		afterFirst := inst
		if err := intent.Submit(&inst, def, intent.SubmitRequest{
			ProposalRevisionID: "revision-1",
			RecordedAt:         recordedAt.Add(time.Minute),
		}); err != nil {
			t.Fatalf("second Submit: %v", err)
		}
		if !reflect.DeepEqual(inst, afterFirst) {
			t.Fatalf("second submit mutated the instance: before=%+v after=%+v", afterFirst, inst)
		}
	})

	t.Run("DRAFT walks preflight and simulate before it submits", func(t *testing.T) {
		inst := submitFixtureInstance(lifecycle.RequestDraft)
		beforeVersion := inst.InstanceVersion
		if err := intent.Submit(&inst, def, intent.SubmitRequest{ProposalRevisionID: "revision-1", RecordedAt: recordedAt}); err != nil {
			t.Fatalf("Submit(DRAFT): %v", err)
		}
		if inst.Lifecycle.Request != lifecycle.RequestSubmitted {
			t.Fatalf("RequestState = %s, want SUBMITTED", inst.Lifecycle.Request)
		}
		// One governed write, whatever the hop count: the version advances
		// by exactly one, not once per intermediate hop.
		if inst.InstanceVersion != beforeVersion+1 {
			t.Fatalf("InstanceVersion = %d, want %d (one write, not one per hop)", inst.InstanceVersion, beforeVersion+1)
		}
	})

	t.Run("PREFLIGHTED walks simulate before it submits", func(t *testing.T) {
		inst := submitFixtureInstance(lifecycle.RequestPreflighted)
		if err := intent.Submit(&inst, def, intent.SubmitRequest{ProposalRevisionID: "revision-1", RecordedAt: recordedAt}); err != nil {
			t.Fatalf("Submit(PREFLIGHTED): %v", err)
		}
		if inst.Lifecycle.Request != lifecycle.RequestSubmitted {
			t.Fatalf("RequestState = %s, want SUBMITTED", inst.Lifecycle.Request)
		}
	})

	t.Run("APPROVED has no declared path to SUBMITTED and is refused", func(t *testing.T) {
		inst := submitFixtureInstance(lifecycle.RequestApproved)
		err := intent.Submit(&inst, def, intent.SubmitRequest{ProposalRevisionID: "revision-1", RecordedAt: recordedAt})
		if !errors.Is(err, intent.ErrSubmitNotSimulated) {
			t.Fatalf("Submit(APPROVED) = %v, want ErrSubmitNotSimulated", err)
		}
		if inst.Lifecycle.Request != lifecycle.RequestApproved {
			t.Fatalf("a refused submit must not move RequestState, got %s", inst.Lifecycle.Request)
		}
	})

	t.Run("every terminal request state refuses to be revived", func(t *testing.T) {
		for _, terminal := range []lifecycle.RequestState{
			lifecycle.RequestCancelled, lifecycle.RequestSuperseded,
			lifecycle.RequestRejected, lifecycle.RequestWithdrawn, lifecycle.RequestClosed,
		} {
			inst := submitFixtureInstance(terminal)
			err := intent.Submit(&inst, def, intent.SubmitRequest{ProposalRevisionID: "revision-1", RecordedAt: recordedAt})
			if !errors.Is(err, intent.ErrSubmitAlreadyTerminal) {
				t.Fatalf("Submit(%s) = %v, want ErrSubmitAlreadyTerminal", terminal, err)
			}
		}
	})

	t.Run("a zero value never means permissive", func(t *testing.T) {
		inst := submitFixtureInstance(lifecycle.RequestSimulated)
		if err := intent.Submit(&inst, def, intent.SubmitRequest{RecordedAt: recordedAt}); err == nil {
			t.Fatal("Submit with no proposal_revision_id succeeded, want a refusal")
		}
		inst2 := submitFixtureInstance(lifecycle.RequestSimulated)
		if err := intent.Submit(&inst2, def, intent.SubmitRequest{ProposalRevisionID: "revision-1"}); err == nil {
			t.Fatal("Submit with a zero-value RecordedAt succeeded, want a refusal")
		}
	})

	t.Run("nil instance and definition mismatch fail closed", func(t *testing.T) {
		if err := intent.Submit(nil, def, intent.SubmitRequest{ProposalRevisionID: "r", RecordedAt: recordedAt}); err == nil {
			t.Fatal("Submit(nil) succeeded, want a refusal")
		}
		inst := submitFixtureInstance(lifecycle.RequestSimulated)
		other := def
		other.Ref = intent.Ref{TypeID: "hcmnext.test.other", Version: 1}
		if err := intent.Submit(&inst, other, intent.SubmitRequest{ProposalRevisionID: "r", RecordedAt: recordedAt}); err == nil {
			t.Fatal("Submit against a mismatched definition succeeded, want a refusal")
		}
	})
}
