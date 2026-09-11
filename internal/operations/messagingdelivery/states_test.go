package delivery

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type stubProvider struct {
	reference string
	accepted  bool
}

func (s stubProvider) Send(context.Context, Delivery) (ProviderResult, error) {
	return ProviderResult{Reference: s.reference, Accepted: s.accepted}, nil
}

func statesTestIntent() Intent {
	return Intent{
		TenantID: "tenant-a", IntentID: "intent/msg-7", RecipientRef: "jane",
		Purpose: "pay-stub", Classification: "INTERNAL", Committed: true,
		IdempotencyKey: "msg-7", CanonicalRequest: []byte("pay-stub:jane"),
		CreatedAt: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

// TestTodo_MSG_007 is the MSG-007 primary test: attempt and recipient
// states stay distinct and walk their honest chains; acceptance satisfies
// nothing downstream; dead ends are retained.
func TestTodo_MSG_007(t *testing.T) {
	dispatcher, err := NewDispatcher(stubProvider{reference: "provider-1", accepted: true}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := dispatcher.Dispatch(context.Background(), statesTestIntent())
	if err != nil {
		t.Fatal(err)
	}
	if dispatched.Attempt.State != AcceptedByProvider {
		t.Fatalf("dispatch ends at %s, want ACCEPTED_BY_PROVIDER", dispatched.Attempt.State)
	}

	// ACCEPTED_BY_PROVIDER satisfies no delivered, read or acknowledged
	// requirement.
	accepted := dispatched.Attempt
	if err := RequireDelivered(accepted); !errors.Is(err, ErrDeliveryRequirement) {
		t.Fatalf("accepted-as-delivered = %v, want ErrDeliveryRequirement", err)
	}
	recipient := Recipient{AttemptID: accepted.ID, RecipientRef: "jane", State: RecipientUnseen}
	if err := RequireRead(recipient); !errors.Is(err, ErrDeliveryRequirement) {
		t.Fatalf("unseen-as-read = %v, want ErrDeliveryRequirement", err)
	}

	// The attempt walks accepted to delivered; the recipient walks unseen
	// to responded, each on its own signals.
	delivered, err := AdvanceAttempt(accepted, EventDeliver, "provider-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireDelivered(delivered); err != nil {
		t.Fatalf("delivered requirement = %v", err)
	}
	for _, event := range []RecipientEvent{EventSeen, EventRead, EventAcknowledge, EventRespond} {
		if recipient, err = AdvanceRecipient(recipient, event); err != nil {
			t.Fatalf("recipient %s: %v", event, err)
		}
	}
	if recipient.State != RecipientResponded || recipient.Revision != 4 {
		t.Fatalf("recipient = %+v, want RESPONDED at revision 4", recipient)
	}
	if err := RequireAcknowledged(recipient); err != nil {
		t.Fatalf("acknowledged requirement = %v", err)
	}

	// Independence both ways: a delivered attempt coexists with an unseen
	// recipient, and read/acknowledged never imply delivery.
	fresh := Recipient{AttemptID: delivered.ID, RecipientRef: "jane", State: RecipientUnseen}
	if err := RequireDelivered(delivered); err != nil {
		t.Fatal(err)
	}
	if err := RequireRead(fresh); !errors.Is(err, ErrDeliveryRequirement) {
		t.Fatalf("unseen-as-read beside delivery = %v", err)
	}

	// Dead ends are retained states: three sibling attempts end
	// delivered, bounced and expired, and every ending ledgers exactly
	// like delivery while nothing moves afterwards.
	tracker := NewTracker()
	if err := tracker.RecordAttempt(delivered); err != nil {
		t.Fatal(err)
	}
	wantEnding := map[AttemptEvent]State{EventBounce: Bounced, EventExpire: Expired}
	for i, event := range []AttemptEvent{EventBounce, EventExpire} {
		intent := statesTestIntent()
		intent.IdempotencyKey = fmt.Sprintf("msg-7-%d", i)
		intent.CanonicalRequest = []byte(fmt.Sprintf("pay-stub:jane:%d", i))
		res, err := dispatcher.Dispatch(context.Background(), intent)
		if err != nil {
			t.Fatal(err)
		}
		ended, err := AdvanceAttempt(res.Attempt, event, "")
		if err != nil {
			t.Fatal(err)
		}
		if ended.State != wantEnding[event] {
			t.Fatalf("%s ended at %s", event, ended.State)
		}
		if err := tracker.RecordAttempt(ended); err != nil {
			t.Fatal(err)
		}
	}
	if len(tracker.AttemptIDs()) != 3 {
		t.Fatalf("tracker holds %d attempts, want all three endings", len(tracker.AttemptIDs()))
	}
	for _, id := range tracker.AttemptIDs() {
		if _, err := tracker.Attempt(id); err != nil {
			t.Fatalf("retained attempt %s missing: %v", id, err)
		}
	}
	bounced, err := AdvanceAttempt(accepted, EventBounce, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdvanceAttempt(bounced, EventDeliver, ""); !errors.Is(err, ErrAttemptTransition) {
		t.Fatalf("bounced redelivery = %v, want ErrAttemptTransition", err)
	}
	if _, err := AdvanceRecipient(Recipient{State: RecipientUnseen}, EventRead); !errors.Is(err, ErrRecipientTransition) {
		t.Fatalf("unseen jump to read accepted")
	}
}

func TestTodo_MSG_007_Race(t *testing.T) {
	tracker := NewTracker()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("attempt-%d", i)
			attempt := Attempt{ID: id, TenantID: "tenant-a", IntentID: "intent/msg-7", State: AcceptedByProvider}
			delivered, err := AdvanceAttempt(attempt, EventDeliver, "provider-1")
			if err != nil {
				errs <- err
				return
			}
			if err := tracker.RecordAttempt(delivered); err != nil {
				errs <- err
				return
			}
			recipient := Recipient{AttemptID: id, RecipientRef: "jane", State: RecipientUnseen}
			seen, err := AdvanceRecipient(recipient, EventSeen)
			if err != nil {
				errs <- err
				return
			}
			if err := tracker.RecordRecipient(seen); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent states = %v", err)
	}
	if len(tracker.AttemptIDs()) != workers {
		t.Fatalf("tracker holds %d attempts, want %d", len(tracker.AttemptIDs()), workers)
	}
}

// TestTodo_MSG_007_Integration proves the dispatcher handoff: dispatched
// attempts enter the honest lifecycle, provider bounces and expiries
// ledger with their refs, and recipients advance beside them.
func TestTodo_MSG_007_Integration(t *testing.T) {
	bouncing := stubProvider{reference: "", accepted: false}
	dispatcher, err := NewDispatcher(bouncing, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	// A refusing provider fails the attempt at dispatch; the failure is
	// retained and still walks no further.
	failed, err := dispatcher.Dispatch(context.Background(), statesTestIntent())
	if err == nil || failed.Attempt.State != Failed {
		t.Fatalf("refused dispatch = %+v, %v; want retained FAILED", failed, err)
	}
	if _, err := AdvanceAttempt(failed.Attempt, EventDeliver, ""); !errors.Is(err, ErrAttemptTransition) {
		t.Fatalf("failed redelivery = %v, want ErrAttemptTransition", err)
	}

	working, err := NewDispatcher(stubProvider{reference: "provider-2", accepted: true}, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	dispatched, err := working.Dispatch(context.Background(), statesTestIntent())
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker()
	accepted := dispatched.Attempt
	if accepted.ProviderRef != "provider-2" {
		t.Fatalf("provider ref = %q, want provider-2", accepted.ProviderRef)
	}
	// A late bounce keeps the provider ref and the terminal state beside
	// an advancing recipient: the two lifecycles never merge.
	bounced, err := AdvanceAttempt(accepted, EventBounce, "")
	if err != nil {
		t.Fatal(err)
	}
	if bounced.ProviderRef != "provider-2" || bounced.State != Bounced {
		t.Fatalf("bounced = %+v, want terminal BOUNCED with its ref", bounced)
	}
	if err := tracker.RecordAttempt(bounced); err != nil {
		t.Fatal(err)
	}
	recipient := Recipient{AttemptID: bounced.ID, RecipientRef: "jane", State: RecipientUnseen}
	seen, err := AdvanceRecipient(recipient, EventSeen)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordRecipient(seen); err != nil {
		t.Fatal(err)
	}
	kept, err := tracker.Attempt(bounced.ID)
	if err != nil || kept.State != Bounced {
		t.Fatalf("bounced attempt disappeared: %+v, %v", kept, err)
	}
	keptRecipient, err := tracker.Recipient(bounced.ID, "jane")
	if err != nil || keptRecipient.State != RecipientSeen {
		t.Fatalf("recipient disappeared: %+v, %v", keptRecipient, err)
	}
}

func TestTodo_MSG_007_Fault(t *testing.T) {
	base := Attempt{ID: "a-1", State: AcceptedByProvider}
	// Events from the wrong state and out of terminal states are refused.
	for _, tc := range []struct {
		state State
		event AttemptEvent
	}{
		{Queued, EventDeliver},
		{Submitted, EventBounce},
		{Delivered, EventFail},
		{Bounced, EventExpire},
		{Failed, EventSubmit},
		{Expired, EventAccept},
	} {
		attempt := base
		attempt.State = tc.state
		if _, err := AdvanceAttempt(attempt, tc.event, ""); !errors.Is(err, ErrAttemptTransition) {
			t.Fatalf("%s + %s accepted", tc.state, tc.event)
		}
	}
	// Recipient signals cannot skip or regress.
	recipient := Recipient{AttemptID: "a-1", RecipientRef: "jane", State: RecipientRead}
	if _, err := AdvanceRecipient(recipient, EventSeen); !errors.Is(err, ErrRecipientTransition) {
		t.Fatalf("recipient regression accepted")
	}
	if _, err := AdvanceRecipient(recipient, EventRespond); !errors.Is(err, ErrRecipientTransition) {
		t.Fatalf("recipient skip accepted")
	}
	// Anonymous records are refused; rewrites and revision replays are refused.
	tracker := NewTracker()
	if err := tracker.RecordAttempt(Attempt{}); !errors.Is(err, ErrAttemptTransition) {
		t.Fatalf("anonymous attempt recorded")
	}
	if err := tracker.RecordAttempt(Attempt{ID: "a-1", State: Queued}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordAttempt(Attempt{ID: "a-1", State: Delivered}); !errors.Is(err, ErrAttemptTransition) {
		t.Fatalf("attempt rewrite accepted")
	}
	if err := tracker.RecordRecipient(Recipient{AttemptID: "a-1", RecipientRef: "jane", State: RecipientSeen, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordRecipient(Recipient{AttemptID: "a-1", RecipientRef: "jane", State: RecipientSeen, Revision: 1}); !errors.Is(err, ErrRecipientTransition) {
		t.Fatalf("recipient replay accepted")
	}
	if _, err := tracker.Attempt("a-9"); !errors.Is(err, ErrAttemptTransition) {
		t.Fatalf("ghost attempt returned")
	}
}
