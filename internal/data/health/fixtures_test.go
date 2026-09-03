package health_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/projection"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	hSchemaRef  = "hcmnext.intents.v1.BusinessIntent@1"
	hStreamKey  = "worker:health-1"
	hProjection = "worker_state"
)

// healthFixture is a migrated schema holding one tenant, one registered
// payload schema, one empty ledger stream and one projection checkpoint at
// sequence zero -- the shared starting point every health test builds
// scenarios on top of.
type healthFixture struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	appender *ledger.Appender
}

func newHealthFixture(t *testing.T) healthFixture {
	t.Helper()
	return newHealthFixtureWithTenant(t, uuid.New())
}

func newHealthFixtureWithTenant(t *testing.T, tenant uuid.UUID) healthFixture {
	t.Helper()
	db := pgtest.New(t)

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "acme-"+tenant.String())

	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, hSchemaRef)

	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture setup: %v", err)
	}
	if err := ledger.EnsureStream(ctx, tx, tenant, hStreamKey, "WORKER", hStreamKey); err != nil {
		t.Fatalf("ensure stream: %v", err)
	}
	if err := projection.EnsureProjection(ctx, tx, tenant, hProjection, hStreamKey); err != nil {
		t.Fatalf("ensure projection: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fixture setup: %v", err)
	}

	return healthFixture{db: db, tenant: tenant, appender: ledger.New()}
}

// commit appends one ledger event, advances the worker_state projection and
// enqueues one outbox message, all atomically -- the "everything current"
// path a real write goes through.
func (f healthFixture) commit(t *testing.T, idempotencyKey string, expectedHead int64, at time.Time) outbox.CommitReceipt {
	t.Helper()
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	receipt, err := outbox.Commit(ctx, tx, f.appender, outbox.CommitRequest{
		Append: ledger.AppendRequest{
			Tenant: f.tenant, StreamKey: hStreamKey, ExpectedHead: expectedHead,
			AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:worker", SchemaRef: hSchemaRef,
			Payload: []byte("event:" + idempotencyKey), OccurredAt: at, EffectiveAt: at,
			CorrelationID: uuid.New(), IdempotencyKey: idempotencyKey,
		},
		Projection: outbox.ProjectionSpec{Name: hProjection},
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "effect:" + idempotencyKey, OrderingKey: hStreamKey, SchemaRef: hSchemaRef,
			Payload: []byte("dispatch:" + idempotencyKey),
		},
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("commit at head %d: %v", expectedHead, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	return receipt
}

// appendOnly appends a ledger event without advancing the projection or
// enqueueing an outbox row, so the projection checkpoint falls behind --
// the fixture for a "stale projection" scenario.
func (f healthFixture) appendOnly(t *testing.T, idempotencyKey string, expectedHead int64, at time.Time) ledger.AppendReceipt {
	t.Helper()
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	receipt, err := f.appender.Append(ctx, tx, ledger.AppendRequest{
		Tenant: f.tenant, StreamKey: hStreamKey, ExpectedHead: expectedHead,
		AssertionClass: ledger.TransactionFact, SourceRef: "hcmnext:worker", SchemaRef: hSchemaRef,
		Payload: []byte("event:" + idempotencyKey), OccurredAt: at, EffectiveAt: at,
		CorrelationID: uuid.New(), IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("append at head %d: %v", expectedHead, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	return receipt
}
