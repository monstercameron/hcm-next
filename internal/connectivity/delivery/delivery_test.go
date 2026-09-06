package delivery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity/delivery"
)

var deliveryAt = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	claims       int
	observations []delivery.Observation
	duplicate    bool
}

func (s *fakeStore) Claim(_ context.Context, _ delivery.Envelope, attempt int) (delivery.Claim, error) {
	s.claims++
	return delivery.Claim{AttemptID: "attempt-" + string(rune('0'+attempt)), Attempt: attempt, AlreadyObserved: s.duplicate}, nil
}
func (s *fakeStore) Observe(_ context.Context, _ delivery.Envelope, _ delivery.Claim, observation delivery.Observation) error {
	s.observations = append(s.observations, observation)
	return nil
}

type fakeTransport struct {
	calls int
	err   error
}

func (t *fakeTransport) Deliver(context.Context, delivery.Envelope) (delivery.ProviderResult, error) {
	t.calls++
	return delivery.ProviderResult{ProviderRef: "provider-1"}, t.err
}

func validEnvelope() delivery.Envelope {
	return delivery.Envelope{TenantID: "tenant-1", IntentID: "intent-1", RecipientRef: "recipient-1", EndpointRef: "endpoint-1", TemplateRef: "template-1", ParametersRef: "params-1", Purpose: "APPROVAL_REQUIRED", Classification: "INTERNAL", Channel: delivery.ChannelEmail, IdempotencyKey: "intent-1:endpoint-1", CorrelationID: "correlation-1", ContentDigest: "sha256:abc", ExpiresAt: deliveryAt.Add(time.Hour)}
}

func TestTodo_SVC_010(t *testing.T) {
	store := &fakeStore{}
	transport := &fakeTransport{}
	runner := delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }}
	observation, err := runner.Deliver(context.Background(), validEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != delivery.StateSubmitted || transport.calls != 1 || len(store.observations) != 1 {
		t.Fatalf("observation=%+v calls=%d records=%d", observation, transport.calls, len(store.observations))
	}
}

func TestTodo_SVC_010_Integration(t *testing.T) {
	store := &fakeStore{duplicate: true}
	transport := &fakeTransport{}
	observation, err := (delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }}).Deliver(context.Background(), validEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != delivery.StateAlreadyObserved || transport.calls != 0 {
		t.Fatalf("duplicate observation=%+v provider calls=%d", observation, transport.calls)
	}
}

func TestTodo_SVC_010_Fault(t *testing.T) {
	store := &fakeStore{}
	transport := &fakeTransport{err: &delivery.ProviderError{Err: errors.New("provider unavailable"), Retryable: true}}
	observation, err := (delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 2}).Deliver(context.Background(), validEnvelope())
	if err == nil || observation.State != delivery.StateFailed || transport.calls != 2 || len(store.observations) != 2 {
		t.Fatalf("fault observation=%+v err=%v calls=%d records=%d", observation, err, transport.calls, len(store.observations))
	}
	transport.err = &delivery.ProviderError{Err: errors.New("accepted but response lost"), Ambiguous: true}
	transport.calls = 0
	store.observations = nil
	observation, err = (delivery.Runner{Store: store, Transport: transport, Clock: func() time.Time { return deliveryAt }, MaxAttempts: 3}).Deliver(context.Background(), validEnvelope())
	if err == nil || observation.State != delivery.StateReviewRequired || transport.calls != 1 {
		t.Fatalf("ambiguous observation=%+v err=%v calls=%d", observation, err, transport.calls)
	}
}

func TestTodo_SVC_010_Security(t *testing.T) {
	envelope := validEnvelope()
	envelope.ContentRef = ""
	if err := envelope.Validate(deliveryAt); err != nil {
		t.Fatal(err)
	}
	// The type itself has no rendered payload, destination address or provider credential.
	if _, err := (delivery.Runner{Store: &fakeStore{}, Transport: &fakeTransport{}, Clock: func() time.Time { return deliveryAt }}).Deliver(context.Background(), delivery.Envelope{TenantID: "tenant-1", IntentID: "intent-1", RecipientRef: "recipient-1", EndpointRef: "endpoint-1", Purpose: "APPROVAL_REQUIRED", Classification: "INTERNAL", Channel: delivery.ChannelEmail, IdempotencyKey: "k", CorrelationID: "c", ContentDigest: "d", ExpiresAt: deliveryAt.Add(time.Hour)}); err == nil {
		t.Fatal("envelope without content/template accepted")
	}
}
