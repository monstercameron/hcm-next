package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

const event001SchemaRef = "hcmnext.events.v1.OutboxEvent@1"

type event001Fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
}

func newEvent001Fixture(t *testing.T) event001Fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-event-001', 'Event 001', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "event-001-"+tenant.String())
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.events.v1.OutboxEvent', 1,
			'hcmnext.events.v1.OutboxEvent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, event001SchemaRef)
	return event001Fixture{db: db, tenant: tenant}
}

func (f event001Fixture) enqueue(t *testing.T, effect, criticality string) outbox.Record {
	t.Helper()
	tx, err := f.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rec, err := outbox.Enqueue(context.Background(), tx, outbox.EnqueueRequest{
		Tenant:         f.tenant,
		OutboxID:       uuid.New(),
		EffectIdentity: effect,
		OrderingKey:    "worker:event-001",
		Criticality:    criticality,
		SchemaRef:      event001SchemaRef,
		Payload:        []byte("payload:" + effect),
	})
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("enqueue %s: %v", effect, err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit %s: %v", effect, err)
	}
	return rec
}

func event001ClaimAt(t *testing.T, f event001Fixture, now time.Time) (outbox.Consumer, outbox.Record) {
	t.Helper()
	f.enqueue(t, "claim-effect", outbox.CriticalityP1)
	c := outbox.NewConsumer(f.db.Conn,
		outbox.WithLease(time.Minute),
		outbox.WithClock(func() time.Time { return now }),
	)
	claimed, err := c.Poll(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d rows, want 1", len(claimed))
	}
	return *c, claimed[0]
}

// TestTodo_EVENT_001 proves that due work is selected in durable priority and
// ordering order, that claims carry a new lease fence, and that a reclaimed
// message remains available for at-least-once delivery.
func TestTodo_EVENT_001(t *testing.T) {
	f := newEvent001Fixture(t)
	f.enqueue(t, "low", outbox.CriticalityP4)
	high := f.enqueue(t, "high", outbox.CriticalityP0)
	c := outbox.NewConsumer(f.db.Conn, outbox.WithClock(func() time.Time { return time.Now().UTC().Add(time.Minute) }))
	claimed, err := c.Claim(context.Background(), f.tenant)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 2 || claimed[0].OutboxID != high.OutboxID {
		t.Fatalf("claim order = %#v, want P0 before P4", claimed)
	}
	if claimed[0].LeaseToken == uuid.Nil || claimed[0].LeaseVersion != 1 {
		t.Fatalf("claim fence = token %s version %d, want non-zero token and version 1", claimed[0].LeaseToken, claimed[0].LeaseVersion)
	}
	if err := c.AckClaim(context.Background(), claimed[0]); err != nil {
		t.Fatalf("ack claim: %v", err)
	}
	if err := c.AckClaim(context.Background(), claimed[1]); err != nil {
		t.Fatalf("ack second claim: %v", err)
	}
}

func TestTodo_EVENT_001_Golden(t *testing.T) {
	f := newEvent001Fixture(t)
	rec := f.enqueue(t, "golden", "")
	if rec.Criticality != outbox.CriticalityP2 {
		t.Fatalf("default criticality = %q, want P2", rec.Criticality)
	}
	var columns int
	if err := f.db.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'outbox'
		  AND column_name IN ('criticality', 'lease_token', 'lease_until', 'lease_version')`, f.db.Schema).Scan(&columns); err != nil {
		t.Fatalf("count claim columns: %v", err)
	}
	if columns != 4 {
		t.Fatalf("claim columns = %d, want 4", columns)
	}
}

func TestTodo_EVENT_001_Race(t *testing.T) {
	f := newEvent001Fixture(t)
	f.enqueue(t, "race", outbox.CriticalityP2)
	left := f.db.NewConn(t)
	right := f.db.NewConn(t)
	now := time.Now().UTC().Add(time.Minute)
	consumers := []*outbox.Consumer{
		outbox.NewConsumer(left, outbox.WithClock(func() time.Time { return now })),
		outbox.NewConsumer(right, outbox.WithClock(func() time.Time { return now })),
	}
	results := make([][]outbox.Record, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range consumers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = consumers[i].Poll(context.Background(), f.tenant)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
	}
	if len(results[0])+len(results[1]) != 1 {
		t.Fatalf("concurrent claim counts = %d + %d, want exactly one", len(results[0]), len(results[1]))
	}
}

func TestTodo_EVENT_001_Integration(t *testing.T) {
	f := newEvent001Fixture(t)
	f.enqueue(t, "same-order-a", outbox.CriticalityP2)
	f.enqueue(t, "same-order-b", outbox.CriticalityP2)
	c := outbox.NewConsumer(f.db.Conn, outbox.WithBatchSize(1), outbox.WithClock(func() time.Time { return time.Now().UTC().Add(time.Minute) }))
	first, err := c.Poll(context.Background(), f.tenant)
	if err != nil || len(first) != 1 {
		t.Fatalf("first poll = %#v, %v; want one row", first, err)
	}
	if first[0].OrderingKey != "worker:event-001" {
		t.Fatalf("ordering key = %q", first[0].OrderingKey)
	}
}

func TestTodo_EVENT_001_Fault(t *testing.T) {
	f := newEvent001Fixture(t)
	claimTime := time.Now().UTC().Add(time.Minute)
	old, first := event001ClaimAt(t, f, claimTime)
	fresh := outbox.NewConsumer(f.db.Conn,
		outbox.WithLease(time.Minute),
		outbox.WithClock(func() time.Time { return claimTime.Add(time.Minute + time.Second) }),
	)
	second, err := fresh.Poll(context.Background(), f.tenant)
	if err != nil || len(second) != 1 {
		t.Fatalf("reclaim = %#v, %v; want one row", second, err)
	}
	if second[0].LeaseVersion != first.LeaseVersion+1 {
		t.Fatalf("lease version = %d, want %d", second[0].LeaseVersion, first.LeaseVersion+1)
	}
	if err := old.AckClaim(context.Background(), first); !errors.Is(err, outbox.ErrLeaseFence) {
		t.Fatal("stale claim unexpectedly acknowledged")
	}
	if err := fresh.AckClaim(context.Background(), second[0]); err != nil {
		t.Fatalf("fresh ack: %v", err)
	}
}

func TestTodo_EVENT_001_Security(t *testing.T) {
	db := pgtest.New(t)
	firstTenant := uuid.New()
	secondTenant := uuid.New()
	for i, tenant := range []uuid.UUID{firstTenant, secondTenant} {
		db.Exec(t, `
			INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
			VALUES ($1, $2, 'cell-security', $3, 'ACTIVE', now())`, tenant, "event-security-"+tenant.String(), "Tenant "+string(rune('A'+i)))
		db.Exec(t, `
			INSERT INTO payload_schema (
				tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
			VALUES ($1, $2, 'hcmnext.events.v1.SecurityEvent', 1,
				'hcmnext.events.v1.SecurityEvent', 'PROTOBUF', 'LEDGER_EVENT')`, tenant, event001SchemaRef)
	}

	for tenant, effect := range map[uuid.UUID]string{firstTenant: "tenant-a", secondTenant: "tenant-b"} {
		tx, err := db.Conn.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if _, err := outbox.Enqueue(context.Background(), tx, outbox.EnqueueRequest{
			Tenant: tenant, EffectIdentity: effect, OrderingKey: "security",
			SchemaRef: event001SchemaRef, Payload: []byte(effect),
		}); err != nil {
			t.Fatalf("enqueue %s: %v", effect, err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatalf("commit %s: %v", effect, err)
		}
	}
	c := outbox.NewConsumer(db.Conn, outbox.WithClock(func() time.Time { return time.Now().UTC().Add(time.Minute) }))
	claimed, err := c.Poll(context.Background(), firstTenant)
	if err != nil || len(claimed) != 1 || claimed[0].EffectIdentity != "tenant-a" {
		t.Fatalf("tenant-scoped claim = %#v, %v", claimed, err)
	}
}

func TestTodo_EVENT_001_Mutation(t *testing.T) {
	f := newEvent001Fixture(t)
	original := f.enqueue(t, "immutable", outbox.CriticalityP2)
	tx, err := f.db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = outbox.Enqueue(context.Background(), tx, outbox.EnqueueRequest{
		Tenant: f.tenant, OutboxID: uuid.New(), EffectIdentity: original.EffectIdentity,
		OrderingKey: original.OrderingKey, Criticality: original.Criticality,
		SchemaRef: original.SchemaRef, Payload: []byte("forged"),
	})
	_ = tx.Rollback(context.Background())
	if !errors.Is(err, outbox.ErrIdentityConflict) {
		t.Fatalf("duplicate mutation error = %v, want ErrIdentityConflict", err)
	}
	if got, err := outbox.Read(context.Background(), f.db.Conn, f.tenant, original.OutboxID); err != nil || string(got.Payload) != "payload:immutable" {
		t.Fatalf("original row = %#v, %v", got, err)
	}
}
