package explorer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/bitemporal"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	schemaComp   = "hcmnext.people.v1.CompensationBase@1"
	authorityRef = "authority:hr"
	streamA      = "worker:1"
	streamB      = "worker:2"
)

var (
	occurredAt  = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	effectiveAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
)

// fixture is a migrated schema with one tenant, one registered payload
// schema, one authority assignment covering it, and two empty worker
// streams. Every migration - including migrations/00014_ledger_hash_chain.sql
// - is applied by pgtest.New, so ledger_hash_chain_link already exists; this
// package needs no side-table DDL of its own.
type fixture struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	digester *hashchain.Digester
	appender *hashchain.Appender
	heads    map[string]int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgtest.New(t)

	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build chain-link registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)

	f := &fixture{db: db, tenant: uuid.New(), digester: digester, appender: hashchain.NewAppender(digester), heads: map[string]int64{}}

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'acme', 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		f.tenant)

	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.people.v1.CompensationBase', 1,
			'hcmnext.people.v1.CompensationBase', 'PROTOBUF', 'LEDGER_EVENT')`,
		f.tenant, schemaComp)

	db.Exec(t, `
		INSERT INTO authority_assignment (
			tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'INTERNAL', 'workforce', timestamptz '2020-01-01T00:00:00Z')`,
		f.tenant, authorityRef)

	for _, stream := range []string{streamA, streamB} {
		f.inTx(t, func(tx dbport.Tx) error {
			return ledger.EnsureStream(context.Background(), tx, f.tenant, stream, "WORKER", stream)
		})
	}

	return f
}

func (f *fixture) inTx(t *testing.T, fn func(dbport.Tx) error) {
	t.Helper()
	if err := f.inTxErr(fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func (f *fixture) inTxErr(fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	return tx.Commit(ctx)
}

// eventSpec is everything a test supplies to append one event. RecordedAt
// defaults to occurredAt when zero, so most tests never have to think about
// it; the bitemporal KnownAt property test sets it explicitly to control
// which knowledge horizon each fact becomes visible at.
type eventSpec struct {
	Stream      string
	Class       ledger.AssertionClass
	Payload     string
	Corrects    *ledger.EventRef
	EffectiveAt time.Time
	RecordedAt  time.Time
}

func (f *fixture) request(spec eventSpec) ledger.AppendRequest {
	class := spec.Class
	if class == "" {
		class = ledger.DomainFact
	}
	eff := spec.EffectiveAt
	if eff.IsZero() {
		eff = effectiveAt
	}
	return ledger.AppendRequest{
		Tenant:         f.tenant,
		StreamKey:      spec.Stream,
		ExpectedHead:   f.heads[spec.Stream],
		AssertionClass: class,
		Authority:      authorityRef,
		SourceRef:      "test:fixture",
		SchemaRef:      schemaComp,
		Payload:        []byte(spec.Payload),
		OccurredAt:     occurredAt,
		EffectiveAt:    eff,
		CorrelationID:  uuid.New(),
		IdempotencyKey: uuid.NewString(),
		Corrects:       spec.Corrects,
	}
}

// appendLinked appends one event - using an Appender pinned to spec's
// RecordedAt (occurredAt when unset), so a bitemporal KnownAt query's
// knowledge horizon is deterministic rather than racing the real wall clock
// every fixture pgtest.New's migrated schema is created under - and, in the
// same transaction, records its hash-chain link: the well-formed calling
// pattern.
func (f *fixture) appendLinked(t *testing.T, spec eventSpec) ledger.AppendReceipt {
	t.Helper()
	req := f.request(spec)
	recordedAt := spec.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = occurredAt
	}
	appender := ledger.New(ledger.WithClock(func() time.Time { return recordedAt }))
	var receipt ledger.AppendReceipt
	err := f.inTxErr(func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = appender.Append(context.Background(), tx, req)
		if appendErr != nil {
			return appendErr
		}
		_, appendErr = f.appender.Append(context.Background(), tx, receipt)
		return appendErr
	})
	if err != nil {
		t.Fatalf("append linked on %s at head %d: %v", spec.Stream, req.ExpectedHead, err)
	}
	f.heads[spec.Stream] = receipt.Sequence
	return receipt
}

// appendGap appends one event WITHOUT recording its hash-chain link,
// simulating a chain that hashchain.Appender never got to extend for this
// sequence. Because ledger_event and ledger_hash_chain_link are both
// append-only (forbid_mutation triggers refuse UPDATE/DELETE at the
// database level), this is the only DB-legal way to produce a broken chain
// in a test: no fixture can corrupt an already-recorded link, only fail to
// record one. digester.Verify then reports ErrChainBroken at exactly this
// event's sequence ("the event exists but no chain link was recorded for
// it"), which is exactly the tamper-detection surface explorer.VerifyChain
// exposes.
func (f *fixture) appendGap(t *testing.T, spec eventSpec) ledger.AppendReceipt {
	t.Helper()
	req := f.request(spec)
	var receipt ledger.AppendReceipt
	f.inTx(t, func(tx dbport.Tx) error {
		var appendErr error
		receipt, appendErr = ledger.Append(context.Background(), tx, req)
		return appendErr
	})
	f.heads[spec.Stream] = receipt.Sequence
	return receipt
}

// decision returns an unrestricted internal/data/bitemporal.Decision for
// this fixture's tenant.
func (f *fixture) bitemporalDecision() bitemporal.Decision {
	return bitemporal.Decision{Tenant: f.tenant}
}
