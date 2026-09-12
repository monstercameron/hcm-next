package intent_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

func cancelDefinition() intent.Definition {
	return intent.Definition{
		Ref:              intent.Ref{TypeID: "hcmnext.test.cancel_fixture", Version: 1},
		Family:           intent.FamilyChangeRequest,
		ApprovalRequired: false,
	}
}

func cancelFixtureInstance(request lifecycle.RequestState, execution lifecycle.ExecutionState) intent.Instance {
	def := cancelDefinition()
	inst := intent.Instance{
		IntentID:   "22222222-2222-2222-2222-222222222222",
		Definition: def.Ref,
		Lifecycle: lifecycle.Dimensions{
			Request:     request,
			Execution:   execution,
			Business:    lifecycle.BusinessNotStarted,
			Consistency: lifecycle.ConsistencyNotApplicable,
			Obligation:  lifecycle.ObligationNotApplicable,
		},
		InstanceVersion: 5,
	}
	if execution == lifecycle.ExecutionCommitted {
		inst.CommitReceiptRef = "receipt:fixture"
	}
	if execution == lifecycle.ExecutionRepairRequired {
		inst.RepairRef = "repair:pre-existing"
	}
	return inst
}

// TestTodo_EP_INTENT_003_Cancellation is the PRIMARY unit test for
// [intent.CancelInstance]: the four GREEN outcomes -- CANCELLED,
// CANCELLATION_PENDING, TOO_LATE and REPAIR_REQUIRED -- are each reached from
// a distinct starting condition and each leaves a distinct, correct
// dimensional tuple. The exact RED clause this proves against is returning
// RequestState=CANCELLED (or any single collapsed status) for all four.
func TestTodo_EP_INTENT_003_Cancellation(t *testing.T) {
	def := cancelDefinition()
	recordedAt := time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)

	t.Run("not yet executing cancels cleanly", func(t *testing.T) {
		for _, execution := range []lifecycle.ExecutionState{
			lifecycle.ExecutionNotPlanned, lifecycle.ExecutionScheduled, lifecycle.ExecutionRevalidating,
		} {
			inst := cancelFixtureInstance(lifecycle.RequestSubmitted, execution)
			disposition, err := intent.CancelInstance(&inst, def, intent.CancelRequest{
				ReasonRef: "reason:withdrawn", RecordedAt: recordedAt,
			})
			if err != nil {
				t.Fatalf("CancelInstance(%s): %v", execution, err)
			}
			if disposition != intent.DispositionCancelled {
				t.Fatalf("disposition = %s, want CANCELLED", disposition)
			}
			if inst.Lifecycle.Request != lifecycle.RequestCancelled {
				t.Fatalf("RequestState = %s, want CANCELLED", inst.Lifecycle.Request)
			}
			if inst.Lifecycle.Execution != lifecycle.ExecutionNotPlanned {
				t.Fatalf("ExecutionState = %s, want NOT_PLANNED", inst.Lifecycle.Execution)
			}
		}
	})

	t.Run("already blocked is formally cancelled without moving execution", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestSubmitted, lifecycle.ExecutionBlocked)
		disposition, err := intent.CancelInstance(&inst, def, intent.CancelRequest{ReasonRef: "reason:withdrawn", RecordedAt: recordedAt})
		if err != nil {
			t.Fatalf("CancelInstance: %v", err)
		}
		if disposition != intent.DispositionCancelled {
			t.Fatalf("disposition = %s, want CANCELLED", disposition)
		}
		if inst.Lifecycle.Request != lifecycle.RequestCancelled || inst.Lifecycle.Execution != lifecycle.ExecutionBlocked {
			t.Fatalf("tuple = %+v, want request CANCELLED execution BLOCKED", inst.Lifecycle)
		}
	})

	t.Run("executing and at a clean safe point cancels: BLOCKED plus CANCELLED", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestSubmitted, lifecycle.ExecutionExecuting)
		disposition, err := intent.CancelInstance(&inst, def, intent.CancelRequest{
			ReasonRef: "reason:withdrawn", Point: intent.CancellationPointClean, RecordedAt: recordedAt,
		})
		if err != nil {
			t.Fatalf("CancelInstance: %v", err)
		}
		if disposition != intent.DispositionCancelled {
			t.Fatalf("disposition = %s, want CANCELLED", disposition)
		}
		if inst.Lifecycle.Request != lifecycle.RequestCancelled {
			t.Fatalf("RequestState = %s, want CANCELLED", inst.Lifecycle.Request)
		}
		if inst.Lifecycle.Execution != lifecycle.ExecutionBlocked {
			t.Fatalf("ExecutionState = %s, want BLOCKED (this execution actually ran)", inst.Lifecycle.Execution)
		}
	})

	for _, point := range []intent.CancellationPoint{intent.CancellationPointMidFlight, intent.CancellationPointUnknown} {
		t.Run("executing mid-flight or unknown is pending, not cancelled", func(t *testing.T) {
			inst := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionExecuting)
			before := inst
			disposition, err := intent.CancelInstance(&inst, def, intent.CancelRequest{
				ReasonRef: "reason:withdrawn", Point: point, RecordedAt: recordedAt,
			})
			if err != nil {
				t.Fatalf("CancelInstance: %v", err)
			}
			if disposition != intent.DispositionCancellationPending {
				t.Fatalf("disposition = %s, want CANCELLATION_PENDING", disposition)
			}
			if !reflect.DeepEqual(inst, before) {
				t.Fatalf("CANCELLATION_PENDING mutated the instance: before=%+v after=%+v", before, inst)
			}
		})
	}

	t.Run("committed is too late and never claims cancelled", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionCommitted)
		before := inst
		disposition, err := intent.CancelInstance(&inst, def, intent.CancelRequest{ReasonRef: "reason:withdrawn", RecordedAt: recordedAt})
		if err != nil {
			t.Fatalf("CancelInstance: %v", err)
		}
		if disposition != intent.DispositionTooLate {
			t.Fatalf("disposition = %s, want TOO_LATE", disposition)
		}
		if !reflect.DeepEqual(inst, before) {
			t.Fatalf("TOO_LATE mutated the instance: before=%+v after=%+v", before, inst)
		}
		if inst.Lifecycle.Request == lifecycle.RequestCancelled {
			t.Fatal("TOO_LATE falsely reports RequestState=CANCELLED: the exact RED clause")
		}
	})

	t.Run("a landed partial effect requires repair, not a clean cancel", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionExecuting)
		disposition, err := intent.CancelInstance(&inst, def, intent.CancelRequest{
			ReasonRef: "reason:withdrawn", Point: intent.CancellationPointPartialEffect,
			RepairRef: "repair:plan-1", RecordedAt: recordedAt,
		})
		if err != nil {
			t.Fatalf("CancelInstance: %v", err)
		}
		if disposition != intent.DispositionRepairRequired {
			t.Fatalf("disposition = %s, want REPAIR_REQUIRED", disposition)
		}
		if inst.Lifecycle.Execution != lifecycle.ExecutionRepairRequired {
			t.Fatalf("ExecutionState = %s, want REPAIR_REQUIRED", inst.Lifecycle.Execution)
		}
		if inst.Lifecycle.Request != lifecycle.RequestApproved {
			t.Fatalf("RequestState moved to %s, want unchanged APPROVED", inst.Lifecycle.Request)
		}
		if inst.RepairRef != "repair:plan-1" {
			t.Fatalf("RepairRef = %q, want the presented repair reference", inst.RepairRef)
		}
	})

	t.Run("a partial effect with no repair reference refuses rather than asserting REPAIR_REQUIRED anyway", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionExecuting)
		_, err := intent.CancelInstance(&inst, def, intent.CancelRequest{
			ReasonRef: "reason:withdrawn", Point: intent.CancellationPointPartialEffect, RecordedAt: recordedAt,
		})
		if !errors.Is(err, intent.ErrCancelRepairRefRequired) {
			t.Fatalf("err = %v, want ErrCancelRepairRefRequired", err)
		}
	})

	t.Run("already REPAIR_REQUIRED stays REPAIR_REQUIRED and unmutated", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionRepairRequired)
		before := inst
		disposition, err := intent.CancelInstance(&inst, def, intent.CancelRequest{ReasonRef: "reason:withdrawn", RecordedAt: recordedAt})
		if err != nil {
			t.Fatalf("CancelInstance: %v", err)
		}
		if disposition != intent.DispositionRepairRequired {
			t.Fatalf("disposition = %s, want REPAIR_REQUIRED", disposition)
		}
		if !reflect.DeepEqual(inst, before) {
			t.Fatalf("already-REPAIR_REQUIRED cancel mutated the instance: before=%+v after=%+v", before, inst)
		}
	})

	t.Run("UNSPECIFIED execution state fails closed rather than guessing", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestSubmitted, lifecycle.ExecutionUnspecified)
		if _, err := intent.CancelInstance(&inst, def, intent.CancelRequest{ReasonRef: "r", RecordedAt: recordedAt}); err == nil {
			t.Fatal("CancelInstance(UNSPECIFIED execution) succeeded, want a refusal")
		}
	})

	t.Run("every terminal request state refuses a second cancellation", func(t *testing.T) {
		for _, terminal := range []lifecycle.RequestState{
			lifecycle.RequestCancelled, lifecycle.RequestSuperseded,
			lifecycle.RequestRejected, lifecycle.RequestWithdrawn, lifecycle.RequestClosed,
		} {
			inst := cancelFixtureInstance(terminal, lifecycle.ExecutionNotPlanned)
			_, err := intent.CancelInstance(&inst, def, intent.CancelRequest{ReasonRef: "r", RecordedAt: recordedAt})
			if !errors.Is(err, intent.ErrCancelAlreadyTerminal) {
				t.Fatalf("CancelInstance(%s) = %v, want ErrCancelAlreadyTerminal", terminal, err)
			}
		}
	})

	t.Run("an unrecognized cancellation point fails closed", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionExecuting)
		_, err := intent.CancelInstance(&inst, def, intent.CancelRequest{
			ReasonRef: "r", Point: intent.CancellationPoint(99), RecordedAt: recordedAt,
		})
		if !errors.Is(err, intent.ErrCancelUnrecognizedPoint) {
			t.Fatalf("err = %v, want ErrCancelUnrecognizedPoint", err)
		}
	})

	t.Run("a zero value never means permissive: an unset RecordedAt fails closed", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned)
		if _, err := intent.CancelInstance(&inst, def, intent.CancelRequest{ReasonRef: "r"}); err == nil {
			t.Fatal("CancelInstance with a zero-value RecordedAt succeeded, want a refusal")
		}
	})

	t.Run("governance is required to cancel an approved intent, exactly as the definition declares", func(t *testing.T) {
		inst := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionNotPlanned)
		if _, err := intent.CancelInstance(&inst, def, intent.CancelRequest{RecordedAt: recordedAt}); err == nil {
			t.Fatal("cancelling an APPROVED intent with no reason_ref succeeded, want a governance refusal")
		}
	})
}

// TestTodo_EP_INTENT_003_CancellationDistinguishability proves the four
// dispositions produce four DIFFERENT dimensional tuples from four intents
// that started in four different execution conditions, read back solely from
// each instance's own Lifecycle field -- the shape the Integration test at
// the application layer repeats against a real Store.
func TestTodo_EP_INTENT_003_CancellationDistinguishability(t *testing.T) {
	def := cancelDefinition()
	recordedAt := time.Date(2026, 9, 12, 11, 30, 0, 0, time.UTC)

	notStarted := cancelFixtureInstance(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned)
	midFlight := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionExecuting)
	committed := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionCommitted)
	partial := cancelFixtureInstance(lifecycle.RequestApproved, lifecycle.ExecutionExecuting)

	results := map[string]lifecycle.Dimensions{}
	dispositions := map[string]intent.CancellationDisposition{}

	d, err := intent.CancelInstance(&notStarted, def, intent.CancelRequest{ReasonRef: "r", RecordedAt: recordedAt})
	mustNoErr(t, err)
	dispositions["not_started"], results["not_started"] = d, notStarted.Lifecycle

	d, err = intent.CancelInstance(&midFlight, def, intent.CancelRequest{ReasonRef: "r", Point: intent.CancellationPointMidFlight, RecordedAt: recordedAt})
	mustNoErr(t, err)
	dispositions["mid_flight"], results["mid_flight"] = d, midFlight.Lifecycle

	d, err = intent.CancelInstance(&committed, def, intent.CancelRequest{ReasonRef: "r", RecordedAt: recordedAt})
	mustNoErr(t, err)
	dispositions["committed"], results["committed"] = d, committed.Lifecycle

	d, err = intent.CancelInstance(&partial, def, intent.CancelRequest{ReasonRef: "r", Point: intent.CancellationPointPartialEffect, RepairRef: "repair:1", RecordedAt: recordedAt})
	mustNoErr(t, err)
	dispositions["partial"], results["partial"] = d, partial.Lifecycle

	want := map[string]intent.CancellationDisposition{
		"not_started": intent.DispositionCancelled,
		"mid_flight":  intent.DispositionCancellationPending,
		"committed":   intent.DispositionTooLate,
		"partial":     intent.DispositionRepairRequired,
	}
	for name, wantDisposition := range want {
		if dispositions[name] != wantDisposition {
			t.Fatalf("%s disposition = %s, want %s", name, dispositions[name], wantDisposition)
		}
	}

	seen := map[lifecycle.Dimensions]string{}
	for name, dims := range results {
		if other, dup := seen[dims]; dup {
			t.Fatalf("%s and %s produced the identical dimensional tuple %+v; the four outcomes are not distinguishable", name, other, dims)
		}
		seen[dims] = name
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
