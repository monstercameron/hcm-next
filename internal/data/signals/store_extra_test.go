package signals

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

func validSubscription() Subscription {
	return Subscription{TenantID: uuid.New(), SubscriptionID: uuid.New(), InstanceID: uuid.New(), NodeID: "await", NodeAttempt: 1,
		EventType: "event", CorrelationKey: "subject", CorrelationValue: "one", ExpectedSchemaRef: "event/v1",
		AcceptedSources: []string{"source"}, Ordering: stepSignal.OrderingNone}
}

func validReceive() ReceiveRequest {
	return ReceiveRequest{Signal: stepSignal.Signal{Tenant: values.TenantId(uuid.NewString()), Source: "source", EventType: "event",
		SchemaRef: "event/v1", CorrelationKey: "subject", CorrelationValue: "one", IdempotencyKey: "key", Payload: []byte(`{"ok":true}`)}}
}

func TestValidationHelpers_RejectMalformedSecurityCoordinates(t *testing.T) {
	base := validSubscription()
	for name, mutate := range map[string]func(*Subscription){
		"nil tenant":                func(s *Subscription) { s.TenantID = uuid.Nil },
		"nil subscription":          func(s *Subscription) { s.SubscriptionID = uuid.Nil },
		"nil instance":              func(s *Subscription) { s.InstanceID = uuid.Nil },
		"missing node":              func(s *Subscription) { s.NodeID = "" },
		"zero attempt":              func(s *Subscription) { s.NodeAttempt = 0 },
		"missing event":             func(s *Subscription) { s.EventType = "" },
		"missing correlation key":   func(s *Subscription) { s.CorrelationKey = "" },
		"missing correlation value": func(s *Subscription) { s.CorrelationValue = "" },
		"missing schema":            func(s *Subscription) { s.ExpectedSchemaRef = "" },
		"missing source":            func(s *Subscription) { s.AcceptedSources = nil },
		"invalid ordering":          func(s *Subscription) { s.Ordering = stepSignal.OrderingExpectation("INVALID") },
	} {
		t.Run("subscription/"+name, func(t *testing.T) {
			in := base
			mutate(&in)
			if err := validateSubscription(in); err == nil {
				t.Fatal("validateSubscription accepted malformed input")
			}
			if err := (Store{}).Subscribe(context.Background(), nil, in); err == nil {
				t.Fatal("Subscribe accepted malformed input")
			}
		})
	}

	receive := validReceive()
	for name, mutate := range map[string]func(*ReceiveRequest){
		"missing tenant":      func(r *ReceiveRequest) { r.Signal.Tenant = "" },
		"bad tenant":          func(r *ReceiveRequest) { r.Signal.Tenant = "not-a-uuid" },
		"missing event":       func(r *ReceiveRequest) { r.Signal.EventType = "" },
		"missing source":      func(r *ReceiveRequest) { r.Signal.Source = "" },
		"missing schema":      func(r *ReceiveRequest) { r.Signal.SchemaRef = "" },
		"missing idempotency": func(r *ReceiveRequest) { r.Signal.IdempotencyKey = "" },
		"empty payload":       func(r *ReceiveRequest) { r.Signal.Payload = nil },
		"invalid payload":     func(r *ReceiveRequest) { r.Signal.Payload = []byte("not-json") },
		"array payload":       func(r *ReceiveRequest) { r.Signal.Payload = []byte(`[1]`) },
	} {
		t.Run("receive/"+name, func(t *testing.T) {
			in := receive
			mutate(&in)
			if err := validateReceive(in); err == nil {
				t.Fatal("validateReceive accepted malformed input")
			}
			if _, err := (Store{}).Receive(context.Background(), nil, in, acceptingVerifierForUnit{}); err == nil {
				t.Fatal("Receive accepted malformed input")
			}
		})
	}
}

type acceptingVerifierForUnit struct{}

func (acceptingVerifierForUnit) Verify(stepSignal.Signal) error { return nil }

func TestReceiveAndCanonicalHelpers_Boundaries(t *testing.T) {
	req := validReceive()
	if _, err := (Store{}).Receive(context.Background(), nil, req, nil); err == nil {
		t.Fatal("Receive accepted a nil verifier")
	}
	if got := payloadDigest([]byte("payload")); got == "" || len(got) != 64 {
		t.Fatalf("payloadDigest() = %q", got)
	}
	when := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	canonical := canonicalSignal{ID: uuid.New(), EventType: "event", Source: "source", CorrelationKey: "key", CorrelationValue: "value", SchemaRef: "schema", DedupToken: "dedupe", SequenceNumber: 3, Payload: []byte(`{"x":1}`), ReceivedAt: when}
	sig, err := canonical.step(values.TenantId(req.Signal.Tenant.String()))
	if err != nil || sig.EventType != canonical.EventType || string(sig.Payload) != string(canonical.Payload) || sig.SequenceNumber != 3 {
		t.Fatalf("canonical.step() = %+v, %v", sig, err)
	}
	sig.Payload[0] = 'X'
	if canonical.Payload[0] == 'X' {
		t.Fatal("canonical.step returned an aliased payload")
	}
	if got := optionalInstant(nil); got.IsSet() {
		t.Fatal("optionalInstant(nil) is not zero")
	}
	got := optionalInstant(&when)
	if !got.IsSet() || !got.Time().Equal(when) {
		t.Fatalf("optionalInstant(time) = %v", got)
	}
	if nullableUUID(uuid.Nil) != nil || nullableUUID(uuid.New()) == nil {
		t.Fatal("nullableUUID boundary is incorrect")
	}
	if len((durableSubscription{AcceptedSources: []byte("not-json")}).step(values.TenantId(req.Signal.Tenant.String())).AcceptedSources) != 0 {
		t.Fatal("invalid accepted_sources JSON decoded as a source")
	}
}
