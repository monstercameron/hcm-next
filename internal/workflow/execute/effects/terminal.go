package effects

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	ledgerport "github.com/monstercameron/hcm-next/internal/ledger"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
)

// PromotionOutcomeSchema names the payload schema [LedgerTerminalWriter]
// records its governed business fact under.
const PromotionOutcomeSchema = "hcmnext.workflow.PromotionOutcome/v1"

// promotionOutcomePayload is the canonical (fixed field order) payload one
// terminal write appends to the ledger.
type promotionOutcomePayload struct {
	WorkflowID         string `json:"workflow_id"`
	PlanDigest         string `json:"plan_digest"`
	InstanceID         string `json:"instance_id"`
	ProposalRevisionID string `json:"proposal_revision_id"`
	ProposalDigest     string `json:"proposal_digest"`
	TerminalCode       string `json:"terminal_code"`
	CorrelationID      string `json:"correlation_id"`
}

// StreamKeyFor names the ledger stream one workflow instance's governed
// outcome is recorded on: one stream per instance, so two unrelated
// instances never contend on a shared compare-and-swap head.
func StreamKeyFor(workflowID, instanceID string) string {
	return "workflow:" + workflowID + ":" + instanceID
}

// correlationNamespace derives a stable uuid from a free-text correlation
// id, for the one call site (the ledger's CorrelationID column) that wants a
// uuid rather than the string the rest of this call threads through
// unchanged.
var correlationNamespace = uuid.MustParse("6b6e2f2a-2f2e-4e8a-9a3c-6a2b7a9d5c11")

func correlationUUID(correlation string) uuid.UUID {
	if correlation == "" {
		return uuid.Nil
	}
	return uuid.NewSHA1(correlationNamespace, []byte(correlation))
}

// LedgerTerminalWriter is the [execute.TerminalWriter] that performs the one
// governed business write a workflow instance's COMPLETE continuation
// raises: it registers the instance's own ledger stream and projection
// checkpoint (both idempotent, matching internal/intent/app/pgstore's own
// AppendIntent shape) and appends the promotion outcome through
// internal/data/outbox.Commit -- one ledger event, one projection advance,
// one outbox message, all inside tx.
//
// It performs no idempotency reservation of its own: the caller
// (internal/workflow/execute's own continuation sink) already wraps this
// call in [idempotency.Guard], so this writer's own AppendRequest.
// IdempotencyKey only has to make a literal retry of this exact write (same
// ledger idempotency key, same bytes) a safe no-op at the ledger's own
// layer too, belt-and-braces with the guard above it.
type LedgerTerminalWriter struct {
	Appender       ledgerport.Appender
	ProjectionName string
	SourceRef      string
}

var _ execute.TerminalWriter = (*LedgerTerminalWriter)(nil)

// Write implements [execute.TerminalWriter].
func (w *LedgerTerminalWriter) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	if w.Appender == nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: LedgerTerminalWriter has no ledger Appender bound")
	}
	streamKey := StreamKeyFor(req.WorkflowID, req.InstanceID.String())

	if err := datalogger.EnsureStream(ctx, tx, req.TenantID, streamKey, "TRANSACTION", req.InstanceID.String()); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: register workflow ledger stream %s: %w", streamKey, err)
	}
	if err := projection.EnsureProjection(ctx, tx, req.TenantID, w.ProjectionName, streamKey); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: register workflow outcome projection checkpoint: %w", err)
	}
	if err := ensurePayloadSchema(ctx, tx, req.TenantID, PromotionOutcomeSchema); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: register workflow outcome payload schema: %w", err)
	}

	payload, err := json.Marshal(promotionOutcomePayload{
		WorkflowID:         req.WorkflowID,
		PlanDigest:         req.PlanDigest,
		InstanceID:         req.InstanceID.String(),
		ProposalRevisionID: req.Proposal.Revision.ProposalRevisionID,
		ProposalDigest:     req.Proposal.Revision.MaterialDigest.Digest,
		TerminalCode:       req.TerminalCode,
		CorrelationID:      req.CorrelationID,
	})
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: encode workflow outcome payload: %w", err)
	}

	receipt, err := outbox.Commit(ctx, tx, w.Appender, outbox.CommitRequest{
		Append: datalogger.AppendRequest{
			Tenant: req.TenantID, StreamKey: streamKey, ExpectedHead: 0,
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      w.SourceRef,
			SchemaRef:      PromotionOutcomeSchema,
			Payload:        payload,
			OccurredAt:     req.RecordedAt,
			EffectiveAt:    req.RecordedAt,
			CorrelationID:  correlationUUID(req.CorrelationID),
			IdempotencyKey: req.IdempotencyKey,
		},
		Projection: outbox.ProjectionSpec{Name: w.ProjectionName},
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "workflow.promotion.apply:" + req.InstanceID.String(),
			OrderingKey:    streamKey,
			SchemaRef:      PromotionOutcomeSchema,
			Payload:        payload,
		},
	})
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: commit workflow outcome ledger write: %w", err)
	}

	return idempotency.ResultIdentity{
		ResultRef: req.TerminalCode,
		EventRef:  fmt.Sprintf("%s@%d", streamKey, receipt.Ledger.Sequence),
	}, nil
}

// ensurePayloadSchema idempotently registers the outcome schema this writer
// records under, matching internal/intent/app/pgstore's own Bootstrap-time
// registration of its envelope schema: a ledger event's schema_ref is a
// foreign key into payload_schema (migrations/00005_ledger.sql), so a first
// write for a tenant that has never seen this schema needs the row to exist
// before Append can succeed. The registration is content-addressed by
// (tenant_id, schema_ref) and only ever inserted once per tenant.
func ensurePayloadSchema(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, schemaRef string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT (tenant_id, schema_ref) DO NOTHING`,
		tenant, schemaRef)
	return err
}
