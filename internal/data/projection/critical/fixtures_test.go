package critical_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/projection/critical"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

var occurredAt = time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

// digestHex returns a well-formed sha256 hex digest (the shape
// migrations/00002's content_digest domain requires) derived from s, so
// fixtures never need to hardcode 64 hex characters by hand.
func digestHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// fixture is a migrated schema holding one tenant with the two payload
// schemas this package's ProtoMapper recognizes already registered.
type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	reader *ledger.Reader
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "acme-"+tenant.String())

	db.Exec(t, `
		INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.IntentInstance', 1, 'hcmnext.intents.v1.IntentInstance', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, critical.SchemaRefIntentInstance)
	db.Exec(t, `
		INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.ProposalRevision', 1, 'hcmnext.intents.v1.ProposalRevision', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, critical.SchemaRefProposalRevision)

	return fixture{db: db, tenant: tenant, reader: ledger.NewReader()}
}

func (f fixture) streamKey(intentID uuid.UUID) string { return "intent:" + intentID.String() }

func (f fixture) ensureStream(t *testing.T, intentID uuid.UUID) {
	t.Helper()
	f.inTx(t, f.db.Conn, func(tx pgx.Tx) error {
		return ledger.EnsureStream(context.Background(), tx, f.tenant, f.streamKey(intentID), "TRANSACTION", intentID.String())
	})
}

func (f fixture) inTx(t *testing.T, conn *pgx.Conn, fn func(pgx.Tx) error) {
	t.Helper()
	if err := f.inTxErr(conn, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func (f fixture) inTxErr(conn *pgx.Conn, fn func(pgx.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// appendAndApply appends req on f.db.Conn and applies the resulting event
// through critical.Apply, in one transaction - the pattern a real caller
// (internal/intent/app/pgstore, or critical.Commit for a fresh append) uses.
func (f fixture) appendAndApply(t *testing.T, mapper critical.Mapper, req ledger.AppendRequest) (ledger.AppendReceipt, critical.ApplyResult) {
	t.Helper()
	return f.appendAndApplyOn(t, f.db.Conn, mapper, req)
}

func (f fixture) appendAndApplyOn(t *testing.T, conn *pgx.Conn, mapper critical.Mapper, req ledger.AppendRequest) (ledger.AppendReceipt, critical.ApplyResult) {
	t.Helper()
	var (
		receipt ledger.AppendReceipt
		result  critical.ApplyResult
	)
	err := f.inTxErr(conn, func(tx pgx.Tx) error {
		var appendErr error
		receipt, appendErr = ledger.Append(context.Background(), tx, req)
		if appendErr != nil {
			return appendErr
		}
		var applyErr error
		result, applyErr = critical.Apply(context.Background(), tx, mapper, critical.ApplyRequest{
			Tenant:    req.Tenant,
			StreamKey: req.StreamKey,
			Sequence:  receipt.Sequence,
			Digest:    receipt.Digest,
			SchemaRef: req.SchemaRef,
			Payload:   req.Payload,
		})
		return applyErr
	})
	if err != nil {
		t.Fatalf("append and apply at expected head %d: %v", req.ExpectedHead, err)
	}
	return receipt, result
}

// applyOnly applies an already-appended event again (a replay, or an event
// the caller supplies out of order), without appending anything new. It
// returns the error rather than failing the test, since these tests exist
// precisely to check that error.
func (f fixture) applyOnly(t *testing.T, mapper critical.Mapper, req critical.ApplyRequest) (critical.ApplyResult, error) {
	t.Helper()
	var result critical.ApplyResult
	err := f.inTxErr(f.db.Conn, func(tx pgx.Tx) error {
		var applyErr error
		result, applyErr = critical.Apply(context.Background(), tx, mapper, req)
		return applyErr
	})
	return result, err
}

func draftLifecycle() *intentsv1.LifecycleDimensions {
	return &intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState_REQUEST_STATE_DRAFT,
		Execution:   intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED,
		Business:    intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED,
		Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE,
		Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE,
	}
}

func submittedLifecycle() *intentsv1.LifecycleDimensions {
	return &intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState_REQUEST_STATE_SUBMITTED,
		Execution:   intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED,
		Business:    intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS,
		Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION,
		Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_PENDING,
	}
}

// newIntentInstanceEvent builds and marshals a well-formed
// hcmnext.intents.v1.IntentInstance event.
func newIntentInstanceEvent(t *testing.T, intentID uuid.UUID, idempotencyKey string, instanceVersion uint64, lifecycle *intentsv1.LifecycleDimensions, at time.Time) []byte {
	t.Helper()
	msg := &intentsv1.IntentInstance{
		IntentId:   intentID.String(),
		Definition: &intentsv1.DefinitionReference{IntentTypeId: "intent.worker.promote", Version: 1},
		CanonicalRequestDigest: &intentsv1.CanonicalDigestReference{
			Digest:      digestHex("request:" + idempotencyKey),
			AlgorithmId: "sha256",
		},
		IdempotencyKey:   idempotencyKey,
		Lifecycle:        lifecycle,
		InstanceVersion:  instanceVersion,
		CreatedAt:        timestamppb.New(at),
		RecordedAt:       timestamppb.New(at),
		LastTransitionAt: timestamppb.New(at),
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal IntentInstance: %v", err)
	}
	return b
}

// newProposalRevisionEvent builds and marshals a well-formed
// hcmnext.intents.v1.ProposalRevision event.
func newProposalRevisionEvent(t *testing.T, intentID uuid.UUID, revision uint64, producedBy string, at time.Time) []byte {
	t.Helper()
	proposalBytes := []byte(fmt.Sprintf("proposal-bytes-%s-%d", intentID, revision))
	msg := &intentsv1.ProposalRevision{
		ProposalRevisionId: uuid.NewString(),
		IntentId:           intentID.String(),
		Revision:           revision,
		MaterialProposalDigest: &intentsv1.CanonicalDigestReference{
			Digest:      digestHex(fmt.Sprintf("material:%s:%d", intentID, revision)),
			AlgorithmId: "sha256",
		},
		Proposal: &intentsv1.TypedPayload{
			Schema:            &intentsv1.SchemaReference{SchemaId: "hcmnext.intents.v1.BusinessIntent", Version: 1},
			ProtobufWireBytes: proposalBytes,
			CanonicalDigest: &intentsv1.CanonicalDigestReference{
				Digest:      digestHex(string(proposalBytes)),
				AlgorithmId: "sha256",
			},
		},
		CreatedBy: &intentsv1.PrincipalReference{PrincipalId: producedBy, Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN},
		CreatedAt: timestamppb.New(at),
	}
	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal ProposalRevision: %v", err)
	}
	return b
}

func creationRequest(streamKey, schemaRef string, tenant uuid.UUID, payload []byte, idempotencyKey string) ledger.AppendRequest {
	return ledger.AppendRequest{
		Tenant:         tenant,
		StreamKey:      streamKey,
		ExpectedHead:   0,
		AssertionClass: ledger.TransactionFact,
		SourceRef:      "hcmnext:intent",
		SchemaRef:      schemaRef,
		Payload:        payload,
		OccurredAt:     occurredAt,
		EffectiveAt:    occurredAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: idempotencyKey,
	}
}

func followOnRequest(streamKey, schemaRef string, tenant uuid.UUID, expectedHead int64, payload []byte, idempotencyKey string) ledger.AppendRequest {
	return ledger.AppendRequest{
		Tenant:         tenant,
		StreamKey:      streamKey,
		ExpectedHead:   expectedHead,
		AssertionClass: ledger.TransactionFact,
		SourceRef:      "hcmnext:intent",
		SchemaRef:      schemaRef,
		Payload:        payload,
		OccurredAt:     occurredAt,
		EffectiveAt:    occurredAt,
		CorrelationID:  uuid.New(),
		IdempotencyKey: idempotencyKey,
	}
}
