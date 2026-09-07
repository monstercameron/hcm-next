package delivery_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/connectivity/delivery"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestPostgresAttemptStoreRejectsUnmappableEnvelope(t *testing.T) {
	store := delivery.PostgresAttemptStore{}
	_, err := store.Claim(context.Background(), delivery.Envelope{TenantID: "not-a-uuid"}, 1)
	if !errors.Is(err, delivery.ErrInvalidStoreEnvelope) {
		t.Fatalf("Claim error = %v, want ErrInvalidStoreEnvelope", err)
	}
}

func TestPostgresAttemptStoreRequiresDatabase(t *testing.T) {
	store := delivery.PostgresAttemptStore{}
	_, err := store.Claim(context.Background(), delivery.Envelope{
		TenantID:    "11111111-1111-1111-1111-111111111111",
		IntentID:    "22222222-2222-2222-2222-222222222222",
		EndpointRef: "33333333-3333-3333-3333-333333333333",
	}, 1)
	if !errors.Is(err, delivery.ErrNoStore) {
		t.Fatalf("Claim error = %v, want ErrNoStore", err)
	}
}

type postgresDeliveryFixture struct {
	tenant    uuid.UUID
	intent    uuid.UUID
	endpoint  uuid.UUID
	recipient uuid.UUID
	envelope  delivery.Envelope
	now       time.Time
}

func newPostgresDeliveryFixture(t *testing.T, key string) (*pgtest.DB, postgresDeliveryFixture) {
	t.Helper()
	db := pgtest.New(t)
	f := postgresDeliveryFixture{
		tenant: uuid.New(), intent: uuid.New(), endpoint: uuid.New(), recipient: uuid.New(),
		now: deliveryAt,
	}
	seedPostgresDeliveryFixture(t, db, &f, key, true)
	return db, f
}

func seedPostgresDeliveryFixture(t *testing.T, db *pgtest.DB, f *postgresDeliveryFixture, key string, includeRecipient bool) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`,
		f.tenant, "delivery-"+key, "delivery "+key, f.now.Add(-time.Hour))
	db.Exec(t, `
		INSERT INTO message_intent (
			tenant_id, message_intent_id, purpose, audience_expression, template_key,
			template_version, classification, urgency, delivery_requirement,
			workflow_ref, correlation_key, created_at)
		VALUES ($1, $2, 'APPROVAL', '{}'::jsonb, 'promotion.approval', 1,
			'INTERNAL', 'NORMAL', 'BEST_EFFORT', 'workflow:delivery', $3, $4)`,
		f.tenant, f.intent, "workflow-"+key, f.now)
	db.Exec(t, `
		INSERT INTO delivery_endpoint (
			tenant_id, endpoint_id, principal_ref, channel, address_digest,
			ownership, verification_state, effective_from)
		VALUES ($1, $2, 'principal-1', 'EMAIL', $3, 'BUSINESS', 'VERIFIED', $4)`,
		f.tenant, f.endpoint, strings.Repeat("a", 64), f.now.Add(-time.Hour))
	if includeRecipient {
		db.Exec(t, `
			INSERT INTO recipient_message (
				tenant_id, recipient_message_id, message_intent_id, recipient_ref,
				endpoint_id, rendered_digest, classification, correlation_key, created_at, updated_at)
			VALUES ($1, $2, $3, 'principal-1', $4, $5, 'INTERNAL', $6, $7, $7)`,
			f.tenant, f.recipient, f.intent, f.endpoint, strings.Repeat("b", 64), "workflow-"+key, f.now)
	}
	f.envelope = delivery.Envelope{
		TenantID: f.tenant.String(), IntentID: f.intent.String(), RecipientRef: "principal-1",
		EndpointRef: f.endpoint.String(), ContentRef: "content:" + key, Purpose: "APPROVAL_REQUIRED",
		Classification: "INTERNAL", Channel: delivery.ChannelEmail, IdempotencyKey: "delivery:" + key,
		CorrelationID: "workflow-" + key, ContentDigest: strings.Repeat("c", 64),
		AvailableAt: f.now, ExpiresAt: f.now.Add(time.Hour),
	}
}

func postgresDeliveryAppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func countDeliveryRows(t *testing.T, db *pgtest.DB, table string, tenant uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", tenant).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func TestPostgresAttemptStoreClaimIsIdempotentUnderConcurrentClaims(t *testing.T) {
	db, fixture := newPostgresDeliveryFixture(t, "concurrent-claim")
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan struct {
		claim delivery.Claim
		err   error
	}, 2)
	for range 2 {
		conn := postgresDeliveryAppConn(t, db)
		go func() {
			<-start
			claim, err := (delivery.PostgresAttemptStore{DB: conn, Clock: func() time.Time { return fixture.now }}).Claim(ctx, fixture.envelope, 1)
			results <- struct {
				claim delivery.Claim
				err   error
			}{claim: claim, err: err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent claims = %+v and %+v, want both successful", first, second)
	}
	if first.claim.AttemptID == "" || first.claim.AttemptID != second.claim.AttemptID {
		t.Fatalf("claim ids = %q and %q, want one durable attempt", first.claim.AttemptID, second.claim.AttemptID)
	}
	if first.claim.Attempt != 1 || second.claim.Attempt != 1 || first.claim.AlreadyObserved || second.claim.AlreadyObserved {
		t.Fatalf("concurrent claims = %+v and %+v, want original unobserved attempt 1", first.claim, second.claim)
	}
	if got := countDeliveryRows(t, db, "delivery_attempt", fixture.tenant); got != 1 {
		t.Fatalf("delivery_attempt rows = %d, want 1", got)
	}
}

func TestPostgresAttemptStoreObserveCommitsReceiptAndSignalTogether(t *testing.T) {
	db, fixture := newPostgresDeliveryFixture(t, "observe-transaction")
	store := delivery.PostgresAttemptStore{DB: postgresDeliveryAppConn(t, db), Clock: func() time.Time { return fixture.now }}
	claim, err := store.Claim(context.Background(), fixture.envelope, 1)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	observation := delivery.Observation{AttemptID: claim.AttemptID, Attempt: claim.Attempt,
		State: delivery.StateSubmitted, ProviderRef: "provider-event-1", RecordedAt: fixture.now}
	if err := store.Observe(context.Background(), fixture.envelope, claim, observation); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if got := countDeliveryRows(t, db, "delivery_receipt", fixture.tenant); got != 1 {
		t.Fatalf("delivery_receipt rows = %d, want 1", got)
	}
	if got := countDeliveryRows(t, db, "workflow_signal", fixture.tenant); got != 1 {
		t.Fatalf("workflow_signal rows = %d, want 1", got)
	}

	// Signal validation happens after the receipt insert. The transaction must
	// roll both writes back when that later step refuses the empty correlation.
	badEnvelope := fixture.envelope
	badEnvelope.CorrelationID = ""
	badObservation := observation
	badObservation.ProviderRef = "provider-event-rollback"
	if err := store.Observe(context.Background(), badEnvelope, claim, badObservation); err == nil {
		t.Fatal("Observe with an empty signal correlation succeeded")
	}
	if got := countDeliveryRows(t, db, "delivery_receipt", fixture.tenant); got != 1 {
		t.Fatalf("delivery_receipt rows after rollback = %d, want 1", got)
	}
	if got := countDeliveryRows(t, db, "workflow_signal", fixture.tenant); got != 1 {
		t.Fatalf("workflow_signal rows after rollback = %d, want 1", got)
	}
}

type postgresRecordingTransport struct {
	calls int
}

func (t *postgresRecordingTransport) Deliver(context.Context, delivery.Envelope) (delivery.ProviderResult, error) {
	t.calls++
	return delivery.ProviderResult{ProviderRef: "provider-event-replay"}, nil
}

func TestPostgresAttemptStoreReplayReturnsOriginalAttemptWithoutSecondReceipt(t *testing.T) {
	db, fixture := newPostgresDeliveryFixture(t, "replay")
	transport := &postgresRecordingTransport{}
	runner := delivery.Runner{
		Store:     delivery.PostgresAttemptStore{DB: postgresDeliveryAppConn(t, db), Clock: func() time.Time { return fixture.now }},
		Transport: transport, Clock: func() time.Time { return fixture.now },
	}
	first, err := runner.Deliver(context.Background(), fixture.envelope)
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	second, err := runner.Deliver(context.Background(), fixture.envelope)
	if err != nil {
		t.Fatalf("replayed delivery: %v", err)
	}
	if first.Attempt != 1 || second.Attempt != 1 || second.State != delivery.StateAlreadyObserved {
		t.Fatalf("first=%+v second=%+v, want replay of original attempt 1", first, second)
	}
	if first.AttemptID == "" || first.AttemptID != second.AttemptID || transport.calls != 1 {
		t.Fatalf("attempt ids=%q/%q provider calls=%d, want one attempt and one provider call", first.AttemptID, second.AttemptID, transport.calls)
	}
	if got := countDeliveryRows(t, db, "delivery_receipt", fixture.tenant); got != 1 {
		t.Fatalf("delivery_receipt rows after replay = %d, want 1", got)
	}
}

func TestPostgresAttemptStoreReadsAreTenantScoped(t *testing.T) {
	db, fixtureA := newPostgresDeliveryFixture(t, "tenant-a")
	fixtureB := fixtureA
	fixtureB.tenant = uuid.New()
	seedPostgresDeliveryFixture(t, db, &fixtureB, "tenant-b", false)

	storeB := delivery.PostgresAttemptStore{DB: postgresDeliveryAppConn(t, db), Clock: func() time.Time { return fixtureB.now }}
	if _, err := storeB.Claim(context.Background(), fixtureB.envelope, 1); !errors.Is(err, delivery.ErrDeliveryTargetMissing) {
		t.Fatalf("cross-tenant Claim error = %v, want ErrDeliveryTargetMissing", err)
	}
	if got := countDeliveryRows(t, db, "delivery_attempt", fixtureB.tenant); got != 0 {
		t.Fatalf("cross-tenant delivery_attempt rows = %d, want 0", got)
	}

	storeA := delivery.PostgresAttemptStore{DB: postgresDeliveryAppConn(t, db), Clock: func() time.Time { return fixtureA.now }}
	if _, err := storeA.Claim(context.Background(), fixtureA.envelope, 1); err != nil {
		t.Fatalf("same-tenant Claim: %v", err)
	}
	if got := countDeliveryRows(t, db, "delivery_attempt", fixtureA.tenant); got != 1 {
		t.Fatalf("same-tenant delivery_attempt rows = %d, want 1", got)
	}
}
