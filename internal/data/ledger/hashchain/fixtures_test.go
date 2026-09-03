package hashchain_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	schemaRef = "hcmnext.intents.v1.BusinessIntent@1"
	streamKey = "worker:1"
)

var (
	occurredAt  = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	effectiveAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
)

// fixture is a migrated schema, plus this package's own ledger_hash_chain_link
// side table (schema.go's SchemaDDL), holding one tenant, one registered
// payload schema and one empty stream.
type fixture struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	digester *hashchain.Digester
	appender *hashchain.Appender
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	db.Exec(t, hashchain.SchemaDDL)

	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build chain-link registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)

	f := fixture{db: db, tenant: uuid.New(), digester: digester, appender: hashchain.NewAppender(digester)}

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

	f.inTx(t, func(tx pgx.Tx) error {
		return datalogger.EnsureStream(context.Background(), tx, f.tenant, streamKey, "WORKER", "worker:1")
	})

	return f
}

// request returns a well-formed append request for the given expected head.
func (f fixture) request(expectedHead int64) datalogger.AppendRequest {
	return datalogger.AppendRequest{
		Tenant:         f.tenant,
		StreamKey:      streamKey,
		ExpectedHead:   expectedHead,
		AssertionClass: datalogger.TransactionFact,
		SourceRef:      "hcmnext:workflow",
		SchemaRef:      schemaRef,
		Payload:        []byte(fmt.Sprintf("event-%d", expectedHead+1)),
		OccurredAt:     occurredAt,
		EffectiveAt:    effectiveAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
	}
}

func (f fixture) inTx(t *testing.T, fn func(pgx.Tx) error) {
	t.Helper()
	if err := f.inTxErr(fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func (f fixture) inTxErr(fn func(pgx.Tx) error) error {
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

// appendLinked appends one ledger event and, in the same transaction, links
// it into the hash chain - the intended calling pattern for this package.
func (f fixture) appendLinked(t *testing.T, req datalogger.AppendRequest) hashchain.ChainedLink {
	t.Helper()
	var link hashchain.ChainedLink
	err := f.inTxErr(func(tx pgx.Tx) error {
		receipt, err := datalogger.Append(context.Background(), tx, req)
		if err != nil {
			return err
		}
		link, err = f.appender.Append(context.Background(), tx, receipt)
		return err
	})
	if err != nil {
		t.Fatalf("append and link at expected head %d: %v", req.ExpectedHead, err)
	}
	return link
}

// appendUnlinked appends a ledger event without recording a chain link,
// simulating an event that hashchain.Appender never got a chance to link -
// one of the "gap in the chain links" fixtures.
func (f fixture) appendUnlinked(t *testing.T, req datalogger.AppendRequest) datalogger.AppendReceipt {
	t.Helper()
	var receipt datalogger.AppendReceipt
	err := f.inTxErr(func(tx pgx.Tx) error {
		var appendErr error
		receipt, appendErr = datalogger.Append(context.Background(), tx, req)
		return appendErr
	})
	if err != nil {
		t.Fatalf("append at expected head %d: %v", req.ExpectedHead, err)
	}
	return receipt
}
