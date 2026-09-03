package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	schemaRef    = "hcmnext.intents.v1.BusinessIntent@1"
	authorityRef = "authority:workday"
	streamKey    = "worker:1"
)

var (
	occurredAt  = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	effectiveAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
)

// fixture is a migrated schema holding one tenant, one registered payload
// schema, one authority assignment and one empty stream.
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

	// The authority governs from 2026-01-01 with no end.
	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'EXTERNAL_SYSTEM', 'workforce.compensation',
			timestamptz '2026-01-01T00:00:00Z')`,
		f.tenant, authorityRef)

	f.inTx(t, func(tx dbport.Tx) error {
		return ledger.EnsureStream(context.Background(), tx, f.tenant, streamKey, "WORKER", "worker:1")
	})

	return f
}

// request returns a well-formed append request for the given expected head.
func (f fixture) request(expectedHead int64) ledger.AppendRequest {
	return ledger.AppendRequest{
		Tenant:         f.tenant,
		StreamKey:      streamKey,
		ExpectedHead:   expectedHead,
		AssertionClass: ledger.TransactionFact,
		SourceRef:      "hcmnext:workflow",
		SchemaRef:      schemaRef,
		Payload:        []byte("promotion-proposed"),
		OccurredAt:     occurredAt,
		EffectiveAt:    effectiveAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	}
}

// inTx runs fn in a transaction on the fixture's own connection and commits.
func (f fixture) inTx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	if err := f.inTxErr(f.db.Conn, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

// inTxErr runs fn in a transaction on the given connection, committing on
// success and rolling back on failure, and returns fn's verdict. It reports
// every failure as a value so that concurrent appenders can call it from their
// own goroutines.
func (f fixture) inTxErr(conn *pgxadapter.Conn, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
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

// append runs one append in its own transaction and returns the receipt.
func (f fixture) append(t *testing.T, req ledger.AppendRequest) (ledger.AppendReceipt, error) {
	t.Helper()
	var receipt ledger.AppendReceipt
	err := f.inTxErr(f.db.Conn, func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = ledger.Append(context.Background(), tx, req)
		return appendErr
	})
	return receipt, err
}

// mustAppend fails the test when the append is refused.
func (f fixture) mustAppend(t *testing.T, req ledger.AppendRequest) ledger.AppendReceipt {
	t.Helper()
	receipt, err := f.append(t, req)
	if err != nil {
		t.Fatalf("append at expected head %d: %v", req.ExpectedHead, err)
	}
	return receipt
}

// sequences returns every sequence recorded on the fixture's stream, in order.
func (f fixture) sequences(t *testing.T) []int64 {
	t.Helper()
	rows, err := f.db.Conn.Query(context.Background(), `
		SELECT sequence FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2
		ORDER BY sequence`, f.tenant, streamKey)
	if err != nil {
		t.Fatalf("read sequences: %v", err)
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var sequence int64
		if err := rows.Scan(&sequence); err != nil {
			t.Fatalf("scan sequence: %v", err)
		}
		out = append(out, sequence)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read sequences: %v", err)
	}
	return out
}

// head returns the stream head sequence and digest.
func (f fixture) head(t *testing.T) (int64, string) {
	t.Helper()
	var sequence int64
	var digest *string
	if err := f.db.QueryRow(context.Background(), `
		SELECT head_sequence, head_digest FROM stream_head
		WHERE tenant_id = $1 AND stream_key = $2`, f.tenant, streamKey).Scan(&sequence, &digest); err != nil {
		t.Fatalf("read stream head: %v", err)
	}
	if digest == nil {
		return sequence, ""
	}
	return sequence, *digest
}
