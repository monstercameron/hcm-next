// Package recovery resolves the result of a local transaction after its
// acknowledgement was lost. It never treats elapsed time as evidence: the
// durable idempotency record, commit/abort receipt, and ledger rows are the
// only sources that can decide the outcome.
package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
)

// Outcome is the result of an evidence-backed recovery lookup.
type Outcome string

const (
	// OutcomeCommitted means a durable commit receipt or completed idempotency
	// record agrees with the ledger evidence.
	OutcomeCommitted Outcome = "COMMITTED"
	// OutcomeNotCommitted means no durable effect exists and an abort receipt,
	// or the absence of all transaction evidence, proves no commit occurred.
	OutcomeNotCommitted Outcome = "NOT_COMMITTED"
	// OutcomeAborted is the more specific spelling used when an abort receipt
	// is present. It is still not committed.
	OutcomeAborted Outcome = "ABORTED"
	// OutcomeAmbiguousRequiresRecovery means evidence is contradictory or a
	// ledger effect exists without its governing receipt. A caller must repair;
	// it must not submit a second effect.
	OutcomeAmbiguousRequiresRecovery Outcome = "AMBIGUOUS_REQUIRES_RECOVERY"

	// OutcomeStillUnknown is retained as a vocabulary alias for callers that
	// use the model's name for an unresolved outcome. Resolution returns the
	// stronger recovery-required outcome whenever evidence is incomplete.
	OutcomeStillUnknown Outcome = OutcomeAmbiguousRequiresRecovery
	// Committed, NotCommitted and AmbiguousRequiresRecovery are concise aliases
	// for adapter code.
	Committed                 = OutcomeCommitted
	NotCommitted              = OutcomeNotCommitted
	AmbiguousRequiresRecovery = OutcomeAmbiguousRequiresRecovery
)

// Request identifies the transaction whose acknowledgement was lost. PlanID
// is the source-reference identity used by transaction.commit. Scope is the
// exact idempotency scope when the caller has it; when omitted, Key and PlanID
// derive the transaction.commit scope.
type Request struct {
	Tenant uuid.UUID
	PlanID string
	Scope  idempotency.Scope
	Key    string
}

// LedgerEventEvidence is the immutable address used to explain a resolution.
type LedgerEventEvidence struct {
	StreamKey      string
	Sequence       int64
	EventID        uuid.UUID
	IdempotencyKey string
}

// Evidence records every durable observation used by Resolve. It is safe for
// telemetry because it contains identities and dimensions, never payloads.
type Evidence struct {
	IdempotencyFound  bool
	IdempotencyStatus idempotency.Status
	CommitReceiptID   uuid.UUID
	AbortReceiptID    uuid.UUID
	LedgerEvents      []LedgerEventEvidence
	ExpectedEvents    int
}

// Result is the evidence-backed outcome. OutcomeAborted and OutcomeNotCommitted
// both mean that retrying the original effect is safe only after the caller's
// normal admission rules allow a new attempt; neither is a committed result.
type Result struct {
	Outcome  Outcome
	Evidence Evidence
}

// ErrInvalidRequest reports a lookup that is not tied to a tenant and a
// transaction identity.
type ErrInvalidRequest struct{ Detail string }

func (e ErrInvalidRequest) Error() string { return "transaction recovery: " + e.Detail }

// ErrEvidenceConflict reports durable rows that cannot describe one atomic
// local outcome. Returning this error is deliberately different from guessing
// a winner.
var ErrEvidenceConflict = errors.New("transaction recovery: contradictory durable evidence")

// Resolve reads the durable evidence for req. It performs no writes and never
// uses a timeout, clock, or retry count as a decision rule.
func Resolve(ctx context.Context, q dbport.Querier, req Request) (Result, error) {
	return Resolver{}.Resolve(ctx, q, req)
}

// Resolver is stateless. The value exists so callers can depend on an
// explicit resolver in composition roots and tests.
type Resolver struct{}

// Resolve implements the TX-005 evidence ordering.
func (Resolver) Resolve(ctx context.Context, q dbport.Querier, req Request) (Result, error) {
	if q == nil {
		return Result{}, ErrInvalidRequest{Detail: "a database reader is required"}
	}
	if req.Tenant == uuid.Nil {
		return Result{}, ErrInvalidRequest{Detail: "tenant is required"}
	}
	scope, err := normalizeScope(req)
	if err != nil {
		return Result{}, err
	}
	if req.PlanID == "" && scope.Key == "" {
		return Result{}, ErrInvalidRequest{Detail: "plan id or idempotency key is required"}
	}

	planID := req.PlanID
	if planID == "" {
		planID = scope.EffectScope
	}
	evidence := Evidence{}
	commit, commitFound, err := readCommitReceipt(ctx, q, req.Tenant, planID)
	if err != nil {
		return Result{}, err
	}
	if commitFound {
		evidence.CommitReceiptID = commit.ReceiptID
		evidence.ExpectedEvents = commit.AppendedEventCount
	}
	abort, abortFound, err := readAbortReceipt(ctx, q, req.Tenant, planID)
	if err != nil {
		return Result{}, err
	}
	if abortFound {
		evidence.AbortReceiptID = abort.ReceiptID
	}
	ledgerEvents, err := readLedgerEvidence(ctx, q, req.Tenant, planID, scope.Key)
	if err != nil {
		return Result{}, err
	}
	evidence.LedgerEvents = ledgerEvents

	var idem *idempotencyObservation
	if scope.Key != "" {
		observed, found, lookupErr := readIdempotency(ctx, q, scope)
		if lookupErr != nil {
			return Result{}, lookupErr
		}
		if found {
			idem = &observed
			evidence.IdempotencyFound = true
			evidence.IdempotencyStatus = observed.Status
			if expected := expectedEventsFromIdentity(observed.Identity); expected > evidence.ExpectedEvents {
				evidence.ExpectedEvents = expected
			}
		}
	}

	if commitFound && abortFound {
		return Result{Outcome: OutcomeAmbiguousRequiresRecovery, Evidence: evidence}, ErrEvidenceConflict
	}
	if idem != nil && idem.Status == idempotency.StatusCompleted && abortFound {
		return Result{Outcome: OutcomeAmbiguousRequiresRecovery, Evidence: evidence}, ErrEvidenceConflict
	}
	if evidence.ExpectedEvents > 0 && len(ledgerEvents) != evidence.ExpectedEvents {
		return Result{Outcome: OutcomeAmbiguousRequiresRecovery, Evidence: evidence}, nil
	}
	if len(ledgerEvents) > 0 && !commitFound && (idem == nil || idem.Status != idempotency.StatusCompleted) {
		return Result{Outcome: OutcomeAmbiguousRequiresRecovery, Evidence: evidence}, nil
	}
	if commitFound || (idem != nil && idem.Status == idempotency.StatusCompleted) {
		return Result{Outcome: OutcomeCommitted, Evidence: evidence}, nil
	}
	if idem != nil && idem.Status == idempotency.StatusReserved {
		return Result{Outcome: OutcomeAmbiguousRequiresRecovery, Evidence: evidence}, nil
	}
	if abortFound {
		return Result{Outcome: OutcomeAborted, Evidence: evidence}, nil
	}
	return Result{Outcome: OutcomeNotCommitted, Evidence: evidence}, nil
}

type receiptEvidence struct {
	ReceiptID          uuid.UUID
	AppendedEventCount int
}

func readCommitReceipt(ctx context.Context, q dbport.Querier, tenant uuid.UUID, planID string) (receiptEvidence, bool, error) {
	if planID == "" {
		return receiptEvidence{}, false, nil
	}
	var out receiptEvidence
	err := q.QueryRow(ctx, `
		SELECT receipt_id, appended_event_count
		FROM transaction_commit_receipt
		WHERE tenant_id = $1 AND plan_id = $2`, tenant, parseUUIDOrNil(planID)).Scan(&out.ReceiptID, &out.AppendedEventCount)
	if errors.Is(err, dbport.ErrNoRows) {
		return receiptEvidence{}, false, nil
	}
	if err != nil {
		// Non-UUID plan identities are valid for the storage-neutral TX-003
		// plan and simply cannot have a row in the UUID-keyed receipt table.
		if strings.Contains(strings.ToLower(err.Error()), "invalid input syntax") {
			return receiptEvidence{}, false, nil
		}
		return receiptEvidence{}, false, fmt.Errorf("transaction recovery: read commit receipt: %w", err)
	}
	return out, true, nil
}

func readAbortReceipt(ctx context.Context, q dbport.Querier, tenant uuid.UUID, planID string) (receiptEvidence, bool, error) {
	if planID == "" {
		return receiptEvidence{}, false, nil
	}
	var out receiptEvidence
	err := q.QueryRow(ctx, `
		SELECT receipt_id
		FROM transaction_abort_receipt
		WHERE tenant_id = $1 AND plan_id = $2`, tenant, parseUUIDOrNil(planID)).Scan(&out.ReceiptID)
	if errors.Is(err, dbport.ErrNoRows) {
		return receiptEvidence{}, false, nil
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "invalid input syntax") {
			return receiptEvidence{}, false, nil
		}
		return receiptEvidence{}, false, fmt.Errorf("transaction recovery: read abort receipt: %w", err)
	}
	return out, true, nil
}

func readLedgerEvidence(ctx context.Context, q dbport.Querier, tenant uuid.UUID, planID, key string) ([]LedgerEventEvidence, error) {
	conditions := []string{"tenant_id = $1"}
	args := []any{tenant}
	arg := 2
	identity := make([]string, 0, 2)
	if key != "" {
		identity = append(identity, fmt.Sprintf("(idempotency_key = $%d OR idempotency_key LIKE $%d)", arg, arg+1))
		args = append(args, key, key+":event:%")
		arg += 2
	}
	if planID != "" {
		identity = append(identity, fmt.Sprintf("source_ref LIKE $%d", arg))
		args = append(args, "transaction-plan:"+planID+":%")
	}
	if len(identity) > 0 {
		conditions = append(conditions, "("+strings.Join(identity, " OR ")+")")
	}
	query := `SELECT stream_key, sequence, event_id, idempotency_key FROM ledger_event WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY stream_key, sequence`
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("transaction recovery: read ledger evidence: %w", err)
	}
	defer rows.Close()
	var out []LedgerEventEvidence
	for rows.Next() {
		var item LedgerEventEvidence
		if err := rows.Scan(&item.StreamKey, &item.Sequence, &item.EventID, &item.IdempotencyKey); err != nil {
			return nil, fmt.Errorf("transaction recovery: scan ledger evidence: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transaction recovery: read ledger evidence rows: %w", err)
	}
	return out, nil
}

type idempotencyObservation struct {
	Status   idempotency.Status
	Identity idempotency.ResultIdentity
}

func readIdempotency(ctx context.Context, q dbport.Querier, scope idempotency.Scope) (idempotencyObservation, bool, error) {
	var status string
	var resultRef, eventRef, effectIdentity, evidenceID *string
	err := q.QueryRow(ctx, `
		SELECT status, result_ref, event_ref, effect_identity, evidence_id
		FROM idempotency_record
		WHERE tenant_id = $1 AND capability_id = $2 AND effect_scope = $3 AND idempotency_key = $4`,
		scope.Tenant, scope.Capability, scope.EffectScope, scope.Key).Scan(&status, &resultRef, &eventRef, &effectIdentity, &evidenceID)
	if errors.Is(err, dbport.ErrNoRows) {
		return idempotencyObservation{}, false, nil
	}
	if err != nil {
		return idempotencyObservation{}, false, fmt.Errorf("transaction recovery: read idempotency receipt: %w", err)
	}
	return idempotencyObservation{
		Status: idempotency.Status(status),
		Identity: idempotency.ResultIdentity{
			ResultRef: deref(resultRef), EventRef: deref(eventRef),
			EffectIdentity: deref(effectIdentity), EvidenceID: deref(evidenceID),
		},
	}, true, nil
}

func expectedEventsFromIdentity(identity idempotency.ResultIdentity) int {
	if identity.ResultRef != "" {
		var receipt struct {
			Events []json.RawMessage `json:"Events"`
		}
		if json.Unmarshal([]byte(identity.ResultRef), &receipt) == nil && len(receipt.Events) > 0 {
			return len(receipt.Events)
		}
	}
	parts := strings.Fields(identity.EventRef)
	if len(parts) > 0 {
		if n, err := strconv.Atoi(parts[0]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func normalizeScope(req Request) (idempotency.Scope, error) {
	scope := req.Scope
	if scope.Tenant == uuid.Nil {
		scope.Tenant = req.Tenant
	}
	if scope.Tenant != req.Tenant {
		return idempotency.Scope{}, ErrInvalidRequest{Detail: "idempotency scope tenant differs from request tenant"}
	}
	if scope.Key == "" {
		scope.Key = req.Key
	}
	if scope.Key != "" && scope.Capability == "" {
		scope.Capability = "transaction.commit"
	}
	if scope.Key != "" && scope.EffectScope == "" {
		scope.EffectScope = req.PlanID
	}
	if scope.Key == "" {
		return scope, nil
	}
	if err := scope.Validate(); err != nil {
		return idempotency.Scope{}, ErrInvalidRequest{Detail: err.Error()}
	}
	return scope, nil
}

func parseUUIDOrNil(value string) uuid.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Version is the package contract version used by architecture tooling.
func Version() int { return 1 }

// Explain returns a bounded description suitable for package telemetry.
func Explain() string {
	return "transaction.recovery v1: evidence-backed local commit outcome resolution"
}
