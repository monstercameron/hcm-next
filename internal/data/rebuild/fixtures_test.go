package rebuild_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/data/rebuild"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	projTable = "rebuild_fixture_proj"
	schemaRef = "hcmnext.intents.v1.BusinessIntent@1"
)

var occurredAt = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

// fixture is a migrated schema holding the one projection table this suite
// replays. Every scenario isolates itself on its own tenant, so a fixture can
// be shared by many subtests without observing each other's rows.
type fixture struct {
	db *pgtest.DB
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	db.Exec(t, `CREATE TABLE `+projTable+` (
		tenant_id uuid NOT NULL,
		stream_key text NOT NULL,
		sequence bigint NOT NULL,
		value text NOT NULL,
		PRIMARY KEY (tenant_id, stream_key, sequence))`)
	return fixture{db: db}
}

// registerSchema registers the payload schema the test's events cite, scoped to
// the tenant. The rebuild's schema check reads payload_schema on the event's
// tenant, so against a live source an event's schema is only accepted if that
// tenant registered it.
func (f fixture) registerSchema(t *testing.T, tenant uuid.UUID) {
	t.Helper()
	f.db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, schemaRef)
}

func inTx(t *testing.T, conn *pgtest.DB, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// insertTenant registers one tenant, the isolation unit for every scenario.
func insertTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "acme-"+tenant.String())
	return tenant
}

// ensureStream registers the ledger stream and the projection checkpoint a
// rebuild scenario needs, in one transaction.
func ensureStream(t *testing.T, db *pgtest.DB, tenant uuid.UUID, stream, projectionName string) {
	t.Helper()
	inTx(t, db, func(tx dbport.Tx) error {
		if err := ledger.EnsureStream(context.Background(), tx, tenant, stream, "WORKER", stream); err != nil {
			return err
		}
		return projection.EnsureProjection(context.Background(), tx, tenant, projectionName, stream)
	})
}

// appendEvent appends one ledger event and returns the ledger's receipt.
func (f fixture) appendEvent(t *testing.T, tenant uuid.UUID, stream string,
	expectedHead int64, class ledger.AssertionClass, corrects *ledger.EventRef) ledger.AppendReceipt {
	t.Helper()
	var receipt ledger.AppendReceipt
	inTx(t, f.db, func(tx dbport.Tx) error {
		var err error
		receipt, err = ledger.Append(context.Background(), tx, ledger.AppendRequest{
			Tenant:         tenant,
			StreamKey:      stream,
			ExpectedHead:   expectedHead,
			AssertionClass: class,
			SourceRef:      "hcmnext:test",
			SchemaRef:      schemaRef,
			Payload:        fmt.Appendf(nil, "event-%d", expectedHead+1),
			OccurredAt:     occurredAt,
			EffectiveAt:    occurredAt,
			CorrelationID:  uuid.New(),
			IdempotencyKey: uuid.NewString(),
			Corrects:       corrects,
		})
		return err
	})
	return receipt
}

func (f fixture) seedRow(t *testing.T, tenant uuid.UUID, stream string, sequence int64, value string) {
	t.Helper()
	f.db.Exec(t, `INSERT INTO `+projTable+` (tenant_id, stream_key, sequence, value) VALUES ($1, $2, $3, $4)`,
		tenant, stream, sequence, value)
}

func (f fixture) rowCount(t *testing.T, tenant uuid.UUID, stream string) int {
	t.Helper()
	var count int
	f.db.QueryRow(context.Background(),
		`SELECT count(*) FROM `+projTable+` WHERE tenant_id = $1 AND stream_key = $2`,
		tenant, stream).Scan(&count)
	return count
}

func (f fixture) valueAt(t *testing.T, tenant uuid.UUID, stream string, sequence int64) string {
	t.Helper()
	var value string
	if err := f.db.QueryRow(context.Background(),
		`SELECT value FROM `+projTable+` WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		tenant, stream, sequence).Scan(&value); err != nil {
		t.Fatalf("read value at sequence %d: %v", sequence, err)
	}
	return value
}

// leftoverShadowTables counts run-unique shadow tables that a finished or
// failed rebuild was supposed to take with it.
func (f fixture) leftoverShadowTables(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.db.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = $1 AND table_name LIKE $2 AND table_name <> $3`,
		f.db.Schema, projTable+"%", projTable).Scan(&count); err != nil {
		t.Fatalf("count leftover shadow tables: %v", err)
	}
	return count
}

// fixtureReducer is the projection this suite rebuilds: one row per event
// keyed by its sequence, and a digest over the tenant's values in sequence
// order, so identical content always digests identically and any content
// difference digests differently.
func fixtureReducer() rebuild.Reducer {
	return rebuild.Reducer{
		Table: projTable,
		Fold: func(ctx context.Context, table string, tx dbport.Tx, ev ledger.EventRecord) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO `+quoted(table)+` (tenant_id, stream_key, sequence, value)
				VALUES ($1, $2, $3, $4)`,
				ev.Tenant, ev.StreamKey, ev.Sequence, fmt.Sprintf("value-%d", ev.Sequence))
			return err
		},
		Digest: func(ctx context.Context, q dbport.Querier, table string, tenant uuid.UUID) (int64, string, error) {
			var rows int64
			var agg *string
			err := q.QueryRow(ctx, `
				SELECT count(*), string_agg(value, '' ORDER BY sequence)
				FROM `+quoted(table)+` WHERE tenant_id = $1`, tenant).Scan(&rows, &agg)
			if err != nil {
				return 0, "", err
			}
			body := ""
			if agg != nil {
				body = *agg
			}
			sum := sha256.Sum256([]byte(body))
			return rows, hex.EncodeToString(sum[:]), nil
		},
	}
}

// fakeSource is a scripted Source: a real ledger stream is sealed and always
// complete, so the replay defects a rebuild must refuse are only visible
// against a feed the ledger itself could never produce.
type fakeSource struct {
	head        int64
	events      []ledger.EventRecord
	registered  map[string]bool
	corrections map[string]rebuild.CorrectionRef
}

func (s fakeSource) Head(ctx context.Context, q dbport.Querier, tenant uuid.UUID, stream string) (int64, error) {
	return s.head, nil
}

func (s fakeSource) Events(ctx context.Context, q dbport.Querier, tenant uuid.UUID, stream string) ([]ledger.EventRecord, error) {
	return s.events, nil
}

func (s fakeSource) SchemaRegistered(ctx context.Context, q dbport.Querier, tenant uuid.UUID, schemaRef string) (bool, error) {
	return s.registered[schemaRef], nil
}

func (s fakeSource) CorrectionTarget(ctx context.Context, q dbport.Querier, ev ledger.EventRecord) (rebuild.CorrectionRef, error) {
	return s.corrections[correctionKey(ev)], nil
}

func correctionKey(ev ledger.EventRecord) string {
	return fmt.Sprintf("%s@%d", ev.StreamKey, ev.Sequence)
}

// ev builds one well-formed ledger event on the given stream.
func ev(tenant uuid.UUID, stream string, sequence int64) ledger.EventRecord {
	return ledger.EventRecord{
		Tenant:         tenant,
		StreamKey:      stream,
		Sequence:       sequence,
		EventID:        uuid.New(),
		AssertionClass: ledger.TransactionFact,
		SourceRef:      "hcmnext:test",
		SchemaRef:      schemaRef,
		Digest:         strings.Repeat("ab", 32),
	}
}

func findingCodes(r rebuild.Report) []string {
	codes := make([]string, len(r.Findings))
	for i, f := range r.Findings {
		codes[i] = f.Code
	}
	return codes
}

func quoted(name string) string { return `"` + name + `"` }
