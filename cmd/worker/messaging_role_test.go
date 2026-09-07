package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/connectivity/delivery"
	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

type recordingMessagingDeliverer struct {
	envelopes   []delivery.Envelope
	observation delivery.Observation
	err         error
}

func (d *recordingMessagingDeliverer) Deliver(_ context.Context, envelope delivery.Envelope) (delivery.Observation, error) {
	d.envelopes = append(d.envelopes, envelope)
	return d.observation, d.err
}

func messagingEnvelopeFor(t *testing.T, tenant uuid.UUID) (outbox.Record, delivery.Envelope) {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	envelope := delivery.Envelope{
		TenantID: tenant.String(), IntentID: uuid.NewString(), RecipientRef: "principal-1",
		EndpointRef: uuid.NewString(), TemplateRef: "promotion.approval@1", ParametersRef: "params-1",
		Purpose: "APPROVAL_REQUIRED", Classification: "INTERNAL", Channel: delivery.ChannelEmail,
		IdempotencyKey: "intent-1:endpoint-1", CorrelationID: "workflow-1",
		ContentDigest: strings.Repeat("c", 64), AvailableAt: now, ExpiresAt: now.Add(time.Hour),
	}
	return outbox.Record{Tenant: tenant, OutboxID: uuid.New(), SchemaRef: MessagingDeliverySchemaRef,
		Payload: mustJSON(t, envelope), UpdatedAt: now}, envelope
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	// JSON marshaling is deliberately kept in the fixture helper so the role
	// itself remains a decoder boundary and never constructs provider payloads.
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestTodo_SVC_010(t *testing.T) {
	tenant := uuid.New()
	msg, want := messagingEnvelopeFor(t, tenant)
	deliverer := &recordingMessagingDeliverer{observation: delivery.Observation{State: delivery.StateSubmitted, AttemptID: "attempt-1", Attempt: 1}}
	role := messagingDeliveryRole{logger: discardLogger(), deliver: deliverer}
	if err := role.dispatch(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.envelopes) != 1 || deliverer.envelopes[0].IntentID != want.IntentID {
		t.Fatalf("delivered envelopes = %+v, want one semantic envelope", deliverer.envelopes)
	}
}

func TestMessagingDeliveryRoleAcksOutboxWithFake(t *testing.T) {
	tenant := uuid.New()
	msg, _ := messagingEnvelopeFor(t, tenant)
	deliverer := &recordingMessagingDeliverer{observation: delivery.Observation{State: delivery.StateSubmitted}}
	disp := &fakeDispatcher{batches: map[uuid.UUID][]outbox.Record{tenant: {msg}}}
	didWork, err := sweepWithHandler(context.Background(), discardLogger(), fakeTenantLister{tenants: []uuid.UUID{tenant}}, disp, (messagingDeliveryRole{logger: discardLogger(), deliver: deliverer}).dispatch)
	if err != nil || !didWork {
		t.Fatalf("sweep didWork=%t err=%v", didWork, err)
	}
	if len(disp.acked) != 1 || len(disp.failed) != 0 {
		t.Fatalf("acked=%v failed=%v, want one ack and no failure", disp.acked, disp.failed)
	}
}

type recordingMessagingTransport struct{ calls int }

func (t *recordingMessagingTransport) Deliver(context.Context, delivery.Envelope) (delivery.ProviderResult, error) {
	t.calls++
	return delivery.ProviderResult{ProviderRef: "provider-event-1"}, nil
}

// TestTodo_SVC_010_Integration proves the command role reaches the durable
// delivery store: a real PostgreSQL attempt, receipt and workflow signal are
// written for one semantic outbox envelope, and the provider boundary is the
// only test double.
func TestTodo_SVC_010_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	msg, envelope := messagingEnvelopeFor(t, tenant)
	now := envelope.AvailableAt
	intentID := uuid.MustParse(envelope.IntentID)
	endpointID := uuid.MustParse(envelope.EndpointRef)
	recipientID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Worker delivery', 'ACTIVE', $3)`,
		tenant, "worker-delivery-"+tenant.String(), now.Add(-time.Hour))
	db.Exec(t, `
		INSERT INTO message_intent (
			tenant_id, message_intent_id, purpose, audience_expression, template_key,
			template_version, classification, urgency, delivery_requirement,
			workflow_ref, correlation_key, created_at)
		VALUES ($1, $2, 'APPROVAL', '{}'::jsonb, 'promotion.approval', 1,
			'INTERNAL', 'NORMAL', 'BEST_EFFORT', 'workflow:worker', $3, $4)`,
		tenant, intentID, envelope.CorrelationID, now)
	db.Exec(t, `
		INSERT INTO delivery_endpoint (
			tenant_id, endpoint_id, principal_ref, channel, address_digest,
			ownership, verification_state, effective_from)
		VALUES ($1, $2, $3, 'EMAIL', $4, 'BUSINESS', 'VERIFIED', $5)`,
		tenant, endpointID, envelope.RecipientRef, strings.Repeat("a", 64), now.Add(-time.Hour))
	db.Exec(t, `
		INSERT INTO recipient_message (
			tenant_id, recipient_message_id, message_intent_id, recipient_ref,
			endpoint_id, rendered_digest, classification, correlation_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'INTERNAL', $7, $8, $8)`,
		tenant, recipientID, intentID, envelope.RecipientRef, endpointID, strings.Repeat("b", 64), envelope.CorrelationID, now)

	provider := &recordingMessagingTransport{}
	runner := delivery.Runner{
		Store:     delivery.PostgresAttemptStore{DB: db.Conn, Provider: "hcmnext.messaging", Clock: func() time.Time { return now }},
		Transport: provider, Clock: func() time.Time { return now },
	}
	role := messagingDeliveryRole{logger: discardLogger(), deliver: runner}
	if err := role.dispatch(context.Background(), msg); err != nil {
		t.Fatalf("dispatch through real store: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	for _, table := range []string{"delivery_attempt", "delivery_receipt", "workflow_signal"} {
		var count int
		if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", tenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s rows = %d, want 1", table, count)
		}
	}
}

func TestTodo_SVC_010_Fault(t *testing.T) {
	tenant := uuid.New()
	msg, _ := messagingEnvelopeFor(t, tenant)
	wantErr := errors.New("provider unavailable")
	deliverer := &recordingMessagingDeliverer{err: wantErr}
	disp := &fakeDispatcher{batches: map[uuid.UUID][]outbox.Record{tenant: {msg}}}
	if _, err := sweepWithHandler(context.Background(), discardLogger(), fakeTenantLister{tenants: []uuid.UUID{tenant}}, disp, (messagingDeliveryRole{logger: discardLogger(), deliver: deliverer}).dispatch); err != nil {
		t.Fatal(err)
	}
	if len(disp.acked) != 0 || len(disp.failed) != 1 {
		t.Fatalf("acked=%v failed=%v, want failed message returned to outbox", disp.acked, disp.failed)
	}
}

func TestTodo_SVC_010_Security(t *testing.T) {
	tenant := uuid.New()
	msg, _ := messagingEnvelopeFor(t, tenant)
	msg.Payload = []byte(`{"tenant_id":"` + tenant.String() + `","intent_id":"x","body":"salary=secret"}`)
	role := messagingDeliveryRole{logger: discardLogger(), deliver: &recordingMessagingDeliverer{}}
	if err := role.dispatch(context.Background(), msg); !errors.Is(err, ErrMessagingPayload) {
		t.Fatalf("dispatch error = %v, want ErrMessagingPayload", err)
	}
}
