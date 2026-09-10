package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func signalIntent(n string) Intent {
	intent := statesTestIntent()
	intent.IntentID = "intent/" + n
	intent.IdempotencyKey = n
	intent.CanonicalRequest = []byte("pay-stub:jane:" + n)
	return intent
}

func dispatchAccepted(t *testing.T, n string) Attempt {
	t.Helper()
	dispatcher, err := NewDispatcher(stubProvider{reference: "provider-" + n, accepted: true}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := dispatcher.Dispatch(context.Background(), signalIntent(n))
	if err != nil {
		t.Fatal(err)
	}
	return res.Attempt
}

func readTo(t *testing.T, recipient Recipient, state RecipientState) Recipient {
	t.Helper()
	order := []RecipientState{RecipientUnseen, RecipientSeen, RecipientRead, RecipientAcknowledged, RecipientResponded}
	events := map[RecipientState]RecipientEvent{
		RecipientSeen: EventSeen, RecipientRead: EventRead,
		RecipientAcknowledged: EventAcknowledge, RecipientResponded: EventRespond,
	}
	started := false
	for _, want := range order {
		if want == recipient.State {
			started = true
		}
		if !started {
			continue
		}
		if want == state {
			break
		}
		next := order[indexOf(order, want)+1]
		var err error
		recipient, err = AdvanceRecipient(recipient, events[next])
		if err != nil {
			t.Fatal(err)
		}
		if next == state {
			break
		}
	}
	if recipient.State != state {
		t.Fatalf("recipient = %s, want %s", recipient.State, state)
	}
	return recipient
}

func indexOf(order []RecipientState, state RecipientState) int {
	for i, s := range order {
		if s == state {
			return i
		}
	}
	return -1
}

// TestTodo_MSG_008 is the MSG-008 primary test: typed signals correlate
// once to intent/task/workflow with evidence, duplicates resume once, and
// unsatisfied mandatory bars never advance.
func TestTodo_MSG_008(t *testing.T) {
	accepted := dispatchAccepted(t, "sig-1")
	delivered, err := AdvanceAttempt(accepted, EventDeliver, "")
	if err != nil {
		t.Fatal(err)
	}
	recipient := readTo(t, Recipient{AttemptID: delivered.ID, RecipientRef: "jane", State: RecipientUnseen}, RecipientRead)
	signaller := NewSignaller()

	satisfied, err := Correlate(Correlation{
		Attempt: delivered, Recipient: recipient,
		IntentID: "intent/sig-1", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{NeedDelivery: true, NeedRead: true})
	if err != nil {
		t.Fatal(err)
	}
	if satisfied.Kind != SignalSatisfied || satisfied.Evidence == "" {
		t.Fatalf("signal = %+v, want evidenced SATISFIED", satisfied)
	}
	first, duplicate, err := signaller.Emit(satisfied)
	if err != nil || duplicate || first.ID != satisfied.ID {
		t.Fatalf("emit = %+v, dup=%v, %v", first, duplicate, err)
	}
	// Redelivery resumes the workflow once: the stored signal returns,
	// flagged duplicate, with no second correlation.
	second, duplicate, err := signaller.Emit(satisfied)
	if err != nil || !duplicate || second != first {
		t.Fatalf("redelivery = %+v, dup=%v, %v; want the stored original", second, duplicate, err)
	}
	if signaller.Count() != 1 {
		t.Fatalf("signaller holds %d signals, want exactly one", signaller.Count())
	}

	// Acknowledged recipients type the stronger signal.
	acked := readTo(t, recipient, RecipientAcknowledged)
	signal, err := Correlate(Correlation{
		Attempt: delivered, Recipient: acked,
		IntentID: "intent/sig-1", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{NeedDelivery: true, NeedAck: true})
	if err != nil {
		t.Fatal(err)
	}
	if signal.Kind != SignalAcknowledged {
		t.Fatalf("signal = %+v, want ACKNOWLEDGED", signal)
	}

	// Unsatisfied mandatory bars never advance the workflow.
	if _, err := Correlate(Correlation{
		Attempt: accepted, Recipient: Recipient{AttemptID: accepted.ID, State: RecipientUnseen},
		IntentID: "intent/sig-1", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{NeedDelivery: true}); !errors.Is(err, ErrRequirementUnmet) {
		t.Fatalf("undelivered advance = %v, want ErrRequirementUnmet", err)
	}
	if _, err := Correlate(Correlation{
		Attempt: delivered, Recipient: Recipient{AttemptID: delivered.ID, State: RecipientSeen},
		IntentID: "intent/sig-1", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{NeedDelivery: true, NeedAck: true}); !errors.Is(err, ErrRequirementUnmet) {
		t.Fatalf("unacknowledged advance = %v, want ErrRequirementUnmet", err)
	}

	// Terminal failure without fallback fails; with a channel it requires
	// fallback, naming the channel in evidence.
	bounced, err := AdvanceAttempt(accepted, EventBounce, "")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := Correlate(Correlation{
		Attempt: bounced, Recipient: Recipient{AttemptID: bounced.ID, State: RecipientUnseen},
		IntentID: "intent/sig-1", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{})
	if err != nil || failed.Kind != SignalFailed {
		t.Fatalf("bounced signal = %+v, %v; want FAILED", failed, err)
	}
	fallback, err := Correlate(Correlation{
		Attempt: bounced, Recipient: Recipient{AttemptID: bounced.ID, State: RecipientUnseen},
		IntentID: "intent/sig-1", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{FallbackChannel: "sms"})
	if err != nil || fallback.Kind != SignalFallbackRequired {
		t.Fatalf("fallback signal = %+v, %v; want FALLBACK_REQUIRED", fallback, err)
	}
	if got := fallback.Evidence; !strings.Contains(got, "sms") {
		t.Fatalf("fallback evidence %q names no channel", got)
	}
}

func TestTodo_MSG_008_Race(t *testing.T) {
	signaller := NewSignaller()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n := fmt.Sprintf("race-%d", i)
			accepted := dispatchAccepted(t, n)
			delivered, err := AdvanceAttempt(accepted, EventDeliver, "")
			if err != nil {
				errs <- err
				return
			}
			recipient := readTo(t, Recipient{AttemptID: delivered.ID, RecipientRef: "jane", State: RecipientUnseen}, RecipientRead)
			signal, err := Correlate(Correlation{
				Attempt: delivered, Recipient: recipient,
				IntentID: "intent/" + n, TaskID: "task/notify", WorkflowID: "wf/onboard",
			}, Requirement{NeedDelivery: true, NeedRead: true})
			if err != nil {
				errs <- err
				return
			}
			if _, _, err := signaller.Emit(signal); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent signal = %v", err)
	}
	if signaller.Count() != workers {
		t.Fatalf("signaller holds %d signals, want %d", signaller.Count(), workers)
	}
}

// TestTodo_MSG_008_Integration walks dispatch to workflow signal without
// leaving the package boundary: states ledger, correlate, emit once.
func TestTodo_MSG_008_Integration(t *testing.T) {
	intent := signalIntent("e2e")
	intent.CreatedAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	dispatcher, err := NewDispatcher(stubProvider{reference: "provider-e2e", accepted: true}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := dispatcher.Dispatch(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker()
	if err := tracker.RecordAttempt(res.Attempt); err != nil {
		t.Fatal(err)
	}
	delivered, err := AdvanceAttempt(res.Attempt, EventDeliver, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordAttempt(delivered); err == nil {
		t.Fatal("rewrote the dispatched attempt instead of advancing it")
	}
	// The tracker keeps the dispatched snapshot; the advanced attempt is
	// a new value the caller files under its own lifecycle. Re-record the
	// advanced copy after removing the stale one is out of scope: instead
	// prove the correlate path consumes the advanced value with evidence.
	recipient := readTo(t, Recipient{AttemptID: delivered.ID, RecipientRef: "jane", State: RecipientUnseen}, RecipientAcknowledged)
	signal, err := Correlate(Correlation{
		Attempt: delivered, Recipient: recipient,
		IntentID: "intent/e2e", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{NeedDelivery: true, NeedAck: true})
	if err != nil {
		t.Fatal(err)
	}
	if signal.Kind != SignalAcknowledged {
		t.Fatalf("signal = %+v, want ACKNOWLEDGED", signal)
	}
	signaller := NewSignaller()
	stored, duplicate, err := signaller.Emit(signal)
	if err != nil || duplicate {
		t.Fatalf("emit = %+v, dup=%v, %v", stored, duplicate, err)
	}
	for _, want := range []string{"intent/e2e", "task/notify", "wf/onboard", "ACKNOWLEDGED"} {
		if !strings.Contains(signal.Evidence+signal.IntentID+signal.TaskID+signal.WorkflowID+string(signal.Kind), want) {
			t.Fatalf("signal loses its correlation: %+v", signal)
		}
	}
}

func TestTodo_MSG_008_Fault(t *testing.T) {
	accepted := dispatchAccepted(t, "fault-1")
	// Recipient from another attempt never correlates.
	_, err := Correlate(Correlation{
		Attempt: accepted, Recipient: Recipient{AttemptID: "attempt/other", State: RecipientRead},
		IntentID: "intent/fault-1", TaskID: "task/notify", WorkflowID: "wf/onboard",
	}, Requirement{})
	if !errors.Is(err, ErrSignalCorrelation) {
		t.Fatalf("foreign recipient = %v, want ErrSignalCorrelation", err)
	}
	// Missing targets never correlate.
	delivered, err := AdvanceAttempt(accepted, EventDeliver, "")
	if err != nil {
		t.Fatal(err)
	}
	recipient := Recipient{AttemptID: delivered.ID, State: RecipientRead}
	if _, err := Correlate(Correlation{Attempt: delivered, Recipient: recipient, TaskID: "task/notify", WorkflowID: "wf/onboard"}, Requirement{}); !errors.Is(err, ErrSignalCorrelation) {
		t.Fatalf("target-less correlation accepted")
	}
	// Anonymous, target-less and evidenced-less signals never emit, and
	// unknown kinds never emit.
	signaller := NewSignaller()
	for name, signal := range map[string]Signal{
		"anonymous": {Kind: SignalSatisfied, IntentID: "i", TaskID: "t", WorkflowID: "w", Evidence: "e"},
		"no target": {ID: "s-1", Kind: SignalSatisfied, Evidence: "e"},
		"no proof":  {ID: "s-1", Kind: SignalSatisfied, IntentID: "i", TaskID: "t", WorkflowID: "w"},
		"no kind":   {ID: "s-1", IntentID: "i", TaskID: "t", WorkflowID: "w", Evidence: "e"},
	} {
		if _, _, err := signaller.Emit(signal); err == nil {
			t.Fatalf("%s signal emitted", name)
		}
	}
	if signaller.Count() != 0 {
		t.Fatalf("faulty signals stored: %d", signaller.Count())
	}
}
