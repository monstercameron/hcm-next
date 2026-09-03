package channelparity

import (
	"strings"
	"testing"
)

func fixture() Request {
	return Request{IntentID: "worker.update", IntentVersion: "3", Subject: "worker-42", Actor: "worker-42", Purpose: "self-service", Assurance: "high", Payload: map[string]string{"effective_date": "2026-10-01", "title": "Senior Analyst"}, Simulation: true, StepUp: true, Confirmation: true, IdempotencyKey: "intent-abc", WorkflowVersion: "wf-7", MessageVersion: "wf-7", Deadline: "2026-10-15", Evidence: []string{"receipt:initial"}}
}

func TestUserFlowCrossChannelParityReturnsSameNormalizedIntentAndOutcome(t *testing.T) {
	r := fixture()
	first, err := Execute(r, Desktop)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range Channels[1:] {
		got, err := Execute(r, ch)
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		if !EqualSemantics(first, got) {
			t.Fatalf("semantic mismatch for %s", ch)
		}
	}
}

func TestTodo_UXFLOW_009_Property(t *testing.T) {
	r := fixture()
	a, _ := Normalize(r)
	r.Payload["effective_date"] = "2026-11-01"
	b, _ := Normalize(r)
	if a.RequestDigest == b.RequestDigest {
		t.Fatal("payload mutation did not alter digest")
	}
}

func TestTodo_UXFLOW_009_Golden(t *testing.T) {
	a, err := Execute(fixture(), Desktop)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Execute(fixture(), Desktop)
	if !EqualSemantics(a, b) {
		t.Fatal("canonical receipt is not deterministic")
	}
}

func TestTodo_UXFLOW_009_Integration(t *testing.T) {
	r, err := Execute(fixture(), Mobile)
	if err != nil {
		t.Fatal(err)
	}
	h, err := Handoff(r, Assisted)
	if err != nil {
		t.Fatal(err)
	}
	if h.Handoff == nil || h.Handoff.IntentDigest != r.Outcome.IntentDigest || h.Handoff.Deadline != fixture().Deadline {
		t.Fatal("handoff lost continuity")
	}
}

func TestTodo_UXFLOW_009_Fault(t *testing.T) {
	r := fixture()
	r.Simulation = false
	for _, ch := range Channels {
		if _, err := Execute(r, ch); err == nil {
			t.Fatalf("%s bypassed simulation", ch)
		}
	}
}

func TestTodo_UXFLOW_009_Security(t *testing.T) {
	r := fixture()
	r.MessageVersion = "wf-old"
	if _, err := Execute(r, SecureMessage); err != ErrStaleMessage {
		t.Fatalf("stale message error = %v", err)
	}
	if _, err := Handoff(Receipt{Channel: Kiosk}, Desktop); err == nil {
		t.Fatal("invalid receipt handed off")
	}
}

func TestTodo_UXFLOW_009_Conformance(t *testing.T) {
	r := fixture()
	for _, ch := range Channels {
		got, err := Execute(r, ch)
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome.Stages[0] != Validate || got.Outcome.Stages[len(got.Outcome.Stages)-1] != Submit {
			t.Fatalf("%s stage contract changed", ch)
		}
	}
}

func TestTodo_UXFLOW_009_Browser(t *testing.T) {
	for _, ch := range []Channel{Desktop, Mobile, Kiosk, SecureMessage} {
		if _, err := Execute(fixture(), ch); err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
	}
}

func TestTodo_UXFLOW_009_Mutation(t *testing.T) {
	r := fixture()
	r.Confirmation = false
	if _, err := Execute(r, Assisted); err == nil {
		t.Fatal("confirmation mutation bypassed gate")
	}
}

func TestTodo_UXFLOW_009_Recovery(t *testing.T) {
	e := NewExecutor()
	r := fixture()
	a, err := e.Apply(r, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Apply(r, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualSemantics(a, b) {
		t.Fatal("reconnect replay changed outcome")
	}
}

// TestTodo_UXFLOW_009_ChannelMatrix keeps the five public routes on the same
// semantic path while allowing each route to retain its presentation channel.
func TestTodo_UXFLOW_009_ChannelMatrix(t *testing.T) {
	r := fixture()
	want, err := Execute(r, Desktop)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range Channels {
		got, err := Execute(r, ch)
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		if got.Channel != ch {
			t.Fatalf("%s: receipt channel = %s", ch, got.Channel)
		}
		if !EqualSemantics(want, got) {
			t.Fatalf("%s changed normalized request or outcome", ch)
		}
	}
}

func TestTodo_UXFLOW_009_StaleMessageActionRejectedAcrossChannelMatrix(t *testing.T) {
	for _, ch := range Channels {
		r := fixture()
		r.MessageVersion = "wf-old"
		_, err := Execute(r, ch)
		if ch == SecureMessage {
			if err != ErrStaleMessage {
				t.Fatalf("%s: stale action error = %v, want %v", ch, err, ErrStaleMessage)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unrelated channel rejected current request: %v", ch, err)
		}
	}
}

func TestTodo_UXFLOW_009_KioskDoesNotLeakPriorParticipant(t *testing.T) {
	prior := fixture()
	prior.Subject, prior.Actor = "worker-prior", "worker-prior"
	prior.Evidence = []string{"receipt:prior"}
	current := fixture()
	current.Subject, current.Actor = "worker-current", "worker-current"
	current.Evidence = []string{"receipt:current"}

	old, err := Execute(prior, Kiosk)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Execute(current, Kiosk)
	if err != nil {
		t.Fatal(err)
	}
	if got.Request.Subject != current.Subject || got.Request.Actor != current.Actor {
		t.Fatalf("kiosk receipt retained prior participant: %+v", got.Request)
	}
	if got.Request.Subject == old.Request.Subject || got.Request.Actor == old.Request.Actor {
		t.Fatal("kiosk receipt leaked prior participant identity")
	}
	if equalStrings(got.Outcome.Evidence, old.Outcome.Evidence) {
		t.Fatal("kiosk receipt retained prior participant evidence")
	}
}

func TestTodo_UXFLOW_009_MobileReconnectDuplicateIsIdempotent(t *testing.T) {
	e := NewExecutor()
	r := fixture()
	first, err := e.Apply(r, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.Apply(r, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualSemantics(first, second) || second.Outcome.IntentDigest != first.Outcome.IntentDigest {
		t.Fatal("mobile reconnect changed or duplicated the applied effect")
	}
	r.Payload["title"] = "Changed after submission"
	if _, err := e.Apply(r, Mobile); err != ErrDuplicate {
		t.Fatalf("mobile replay with changed payload error = %v, want %v", err, ErrDuplicate)
	}
}

func TestTodo_UXFLOW_009_UnsupportedStageHandoffIsSafe(t *testing.T) {
	r := fixture()
	receipt, err := Execute(r, Mobile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Handoff(receipt, Channel("unsupported-stage"))
	if err == nil || !strings.Contains(err.Error(), "unsupported handoff target") {
		t.Fatalf("unsupported handoff error = %v", err)
	}
	if receipt.Outcome.Deadline != r.Deadline || receipt.Outcome.IntentDigest == "" {
		t.Fatal("failed handoff did not leave source receipt intact")
	}
}
