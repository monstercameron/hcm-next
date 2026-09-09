package lineage_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	schemaRef    = "hcmnext.intents.v1.BusinessIntent@1"
	authorityRef = "authority:workday"
	streamKey    = "worker:1"
	otherStream  = "worker:2"
)

var (
	occurredAt  = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	effectiveAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
)

// fixture is a migrated schema holding one tenant, one registered payload
// schema, one authority assignment, and two empty streams.
type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	f := fixture{db: db, tenant: uuid.New()}

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'acme', 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		f.tenant)

	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		f.tenant, schemaRef)

	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'EXTERNAL_SYSTEM', 'workforce.compensation',
			timestamptz '2026-01-01T00:00:00Z')`,
		f.tenant, authorityRef)

	for _, key := range []string{streamKey, otherStream} {
		f.inTx(t, func(tx dbport.Tx) error {
			return datalogger.EnsureStream(context.Background(), tx, f.tenant, key, "WORKER", key)
		})
	}

	return f
}

// request returns a well-formed TRANSACTION_FACT append request on stream
// for the given expected head.
func (f fixture) request(stream string, expectedHead int64, payload string) datalogger.AppendRequest {
	return datalogger.AppendRequest{
		Tenant:         f.tenant,
		StreamKey:      stream,
		ExpectedHead:   expectedHead,
		AssertionClass: datalogger.TransactionFact,
		SourceRef:      "hcmnext:workflow",
		SchemaRef:      schemaRef,
		Payload:        []byte(payload),
		OccurredAt:     occurredAt,
		EffectiveAt:    effectiveAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	}
}

// correction returns a CORRECTION request on stream targeting corrects, with
// reason carried in the payload - the ledger has no reason field of its own
// (package doc).
func (f fixture) correction(stream string, expectedHead int64, corrects lineage.EventRef, reason string) datalogger.AppendRequest {
	req := f.request(stream, expectedHead, "correction: "+reason)
	req.AssertionClass = datalogger.Correction
	req.Corrects = &corrects
	return req
}

func (f fixture) inTx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	if err := f.inTxErr(fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func (f fixture) inTxErr(fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}
	return tx.Commit(ctx)
}

// append runs one internal/data/ledger.Append (bypassing lineage.Append) in
// its own transaction, for building fixtures that lineage's own validation
// would refuse - e.g. a raw correction to a dangling target used only to
// prove ValidateCorrectionTarget and Append reject what a bare Append would
// have allowed.
func (f fixture) append(t *testing.T, req datalogger.AppendRequest) (datalogger.AppendReceipt, error) {
	t.Helper()
	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = datalogger.Append(context.Background(), tx, req)
		return appendErr
	})
	return receipt, err
}

func (f fixture) mustAppend(t *testing.T, req datalogger.AppendRequest) datalogger.AppendReceipt {
	t.Helper()
	receipt, err := f.append(t, req)
	if err != nil {
		t.Fatalf("append on %s at expected head %d: %v", req.StreamKey, req.ExpectedHead, err)
	}
	return receipt
}

// appendLineage runs lineage.Append - the path under test for corrections -
// in its own transaction.
func (f fixture) appendLineage(t *testing.T, req datalogger.AppendRequest) (datalogger.AppendReceipt, error) {
	t.Helper()
	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = lineage.Append(context.Background(), tx, f.tenant, req)
		return appendErr
	})
	return receipt, err
}

func (f fixture) mustAppendLineage(t *testing.T, req datalogger.AppendRequest) datalogger.AppendReceipt {
	t.Helper()
	receipt, err := f.appendLineage(t, req)
	if err != nil {
		t.Fatalf("lineage append on %s at expected head %d: %v", req.StreamKey, req.ExpectedHead, err)
	}
	return receipt
}

func ref(stream string, sequence int64) lineage.EventRef {
	return lineage.EventRef{StreamKey: stream, Sequence: sequence}
}

// otherTenant creates and returns a second tenant, with the same payload
// schema registered and streamKey/otherStream provisioned, so cross-tenant
// fixtures do not have to repeat setup.
func (f fixture) otherTenant(t *testing.T) uuid.UUID {
	t.Helper()
	other := uuid.New()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'other', 'cell-local', 'Other', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, other)
	f.db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		other, schemaRef)
	for _, key := range []string{streamKey, otherStream} {
		f.inTx(t, func(tx dbport.Tx) error {
			return datalogger.EnsureStream(context.Background(), tx, other, key, "WORKER", key)
		})
	}
	return other
}

// mustAppendTenant appends req under an explicit tenant (overriding
// req.Tenant), bypassing lineage.Append, for building fixtures under a
// second tenant.
func (f fixture) mustAppendTenant(t *testing.T, tenant uuid.UUID, req datalogger.AppendRequest) datalogger.AppendReceipt {
	t.Helper()
	req.Tenant = tenant
	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = datalogger.Append(context.Background(), tx, req)
		return appendErr
	})
	if err != nil {
		t.Fatalf("append under tenant %s: %v", tenant, err)
	}
	return receipt
}

// readPayload returns the payload bytes recorded at (stream, sequence) for
// f.tenant, as a string, for content and reason assertions.
func (f fixture) readPayload(t *testing.T, stream string, sequence int64) string {
	t.Helper()
	var payload []byte
	if err := f.db.QueryRow(context.Background(), `
		SELECT payload FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		f.tenant, stream, sequence).Scan(&payload); err != nil {
		t.Fatalf("read payload %s@%d: %v", stream, sequence, err)
	}
	return string(payload)
}

// sequences returns every sequence recorded on stream for f.tenant, in
// order.
func (f fixture) sequences(t *testing.T, stream string) []int64 {
	t.Helper()
	rows, err := f.db.Conn.Query(context.Background(), `
		SELECT sequence FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 ORDER BY sequence`,
		f.tenant, stream)
	if err != nil {
		t.Fatalf("read sequences: %v", err)
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("scan sequence: %v", err)
		}
		out = append(out, seq)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read sequences: %v", err)
	}
	return out
}
