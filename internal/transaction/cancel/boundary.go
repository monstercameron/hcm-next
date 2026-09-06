// Package cancel owns the irreversible boundary between a governed local
// commit and a cancellation request. It serializes both decisions on the
// transaction identity and records the decision as append-only evidence.
package cancel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/transaction/plan"
)

const (
	OutcomeCancelled = "CANCELLED"
	OutcomeCommitted = "COMMITTED"

	BoundaryNone         = ""
	BoundaryBeforeCommit = "BEFORE_COMMIT"
	BoundaryAfterCommit  = "AFTER_COMMIT"

	evidenceControlKey = "transaction.commit.cancellation"
)

var (
	ErrInvalidRequest = errors.New("transaction cancellation: invalid request")
	ErrCommitConflict = errors.New("transaction cancellation: commit and cancellation evidence conflict")
	ErrCompensation   = errors.New("transaction cancellation: compensation launch failed")
)

// Request is the immutable identity of a cancellation decision. PlanDigest is
// retained in evidence even when the caller has no transaction_plan registry
// row, because the storage-neutral transaction plan is still the authority
// being cancelled.
type Request struct {
	Tenant         uuid.UUID
	PlanID         string
	PlanDigest     string
	IdempotencyKey string
	RequestedBy    string
	Reason         string
	RequestedAt    time.Time

	// CompensationPath is a governed TX-007 path reference. The callback is
	// deliberately supplied by the composition root; this package never calls
	// an external system and never invents correction material.
	CompensationPath string
}

// NewRequest binds a cancellation request to an immutable prepared plan.
func NewRequest(p plan.TransactionPlan, requestedBy, reason string, at time.Time) Request {
	return Request{
		Tenant: tenantID(p.Tenant), PlanID: p.PlanID, PlanDigest: p.Digest,
		IdempotencyKey: p.IdempotencyKey, RequestedBy: requestedBy,
		Reason: reason, RequestedAt: at.UTC(),
	}
}

func tenantID(value interface{ String() string }) uuid.UUID {
	id, _ := uuid.Parse(value.String())
	return id
}

// CompensationRequest is the only input supplied to a post-commit governed
// compensation or correction launcher. The committed transaction remains the
// source of truth; a launcher may append a correction but may not rewrite it.
type CompensationRequest struct {
	Request        Request
	CommitIdentity string
}

// Compensator launches the already-governed compensation/correction path.
type Compensator func(context.Context, CompensationRequest) error

// CommitRecord is the minimal result the boundary needs from the commit
// coordinator. The full transactioncommit.Receipt stays in that package and
// is returned by its additive adapter.
type CommitRecord struct{ Identity string }

// CommitFunc performs all local commit writes in tx and returns its durable
// identity. It must not call Commit or Rollback; Coordinator owns both.
type CommitFunc func(context.Context, dbport.Tx) (CommitRecord, error)

// Result is the typed, mutually exclusive boundary outcome.
type Result struct {
	Outcome              string
	Boundary             string
	CommitIdentity       string
	CancellationEvidence uuid.UUID
	CompensationLaunched bool
}

// Coordinator serializes commit and cancellation for one plan identity.
// Database transactions are deliberately caller-independent so two workers
// on separate connections resolve a race through PostgreSQL, not process
// memory.
type Coordinator struct {
	DB          dbport.Beginner
	Clock       func() time.Time
	Compensator Compensator
}

// Commit runs commitFn while holding the same transaction-scoped lock used by
// Cancel. A committed cancellation evidence row short-circuits commit before
// commitFn can create ledger or outbox rows.
func (c Coordinator) Commit(ctx context.Context, req Request, commitFn CommitFunc) (Result, error) {
	if err := validate(req); err != nil {
		return Result{}, err
	}
	if c.DB == nil || commitFn == nil {
		return Result{}, fmt.Errorf("%w: database and commit function are required", ErrInvalidRequest)
	}
	tx, err := c.begin(ctx, req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lock(ctx, tx, req); err != nil {
		return Result{}, err
	}
	if evidence, found, err := findEvidence(ctx, tx, req, BoundaryBeforeCommit); err != nil {
		return Result{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return Result{}, fmt.Errorf("transaction cancellation: commit cancelled decision: %w", err)
		}
		return Result{Outcome: OutcomeCancelled, Boundary: BoundaryBeforeCommit,
			CommitIdentity: plannedIdentity(req), CancellationEvidence: evidence}, nil
	}
	record, err := commitFn(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	if record.Identity == "" {
		record.Identity = plannedIdentity(req)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("transaction cancellation: commit acknowledgement is ambiguous: %w", err)
	}
	return Result{Outcome: OutcomeCommitted, Boundary: BoundaryNone, CommitIdentity: record.Identity}, nil
}

// Cancel takes the lock, observes the durable commit boundary, and records an
// append-only decision. If the commit already crossed the boundary, the
// committed outcome is returned unchanged and the governed compensation path
// is launched after the evidence transaction commits.
func (c Coordinator) Cancel(ctx context.Context, req Request) (Result, error) {
	if err := validate(req); err != nil {
		return Result{}, err
	}
	if c.DB == nil {
		return Result{}, fmt.Errorf("%w: database is required", ErrInvalidRequest)
	}
	tx, err := c.begin(ctx, req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lock(ctx, tx, req); err != nil {
		return Result{}, err
	}
	identity, committed, err := committedIdentity(ctx, tx, req)
	if err != nil {
		return Result{}, err
	}
	boundary := BoundaryBeforeCommit
	if committed {
		boundary = BoundaryAfterCommit
		if identity == "" {
			identity = plannedIdentity(req)
		}
	} else {
		identity = plannedIdentity(req)
	}
	evidence, err := recordEvidence(ctx, tx, req, boundary, identity)
	if err != nil {
		return Result{}, err
	}
	if boundary == BoundaryBeforeCommit {
		if err := recordAbortReceipt(ctx, tx, req, evidence, identity); err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("transaction cancellation: commit decision: %w", err)
	}
	result := Result{Outcome: OutcomeCancelled, Boundary: boundary, CommitIdentity: identity, CancellationEvidence: evidence}
	if boundary == BoundaryAfterCommit && req.CompensationPath != "" {
		if c.Compensator == nil {
			return result, fmt.Errorf("%w: path %s has no launcher", ErrCompensation, req.CompensationPath)
		}
		if err := c.Compensator(ctx, CompensationRequest{Request: req, CommitIdentity: identity}); err != nil {
			return result, fmt.Errorf("%w: %s: %v", ErrCompensation, req.CompensationPath, err)
		}
		result.CompensationLaunched = true
	}
	if committed {
		result.Outcome = OutcomeCommitted
	}
	return result, nil
}

func (c Coordinator) begin(ctx context.Context, req Request) (dbport.Tx, error) {
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("transaction cancellation: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, req.Tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func validate(req Request) error {
	switch {
	case req.Tenant == uuid.Nil:
		return fmt.Errorf("%w: tenant is required", ErrInvalidRequest)
	case strings.TrimSpace(req.PlanID) == "":
		return fmt.Errorf("%w: plan id is required", ErrInvalidRequest)
	case strings.TrimSpace(req.PlanDigest) == "":
		return fmt.Errorf("%w: plan digest is required", ErrInvalidRequest)
	case strings.TrimSpace(req.RequestedBy) == "":
		return fmt.Errorf("%w: requester is required", ErrInvalidRequest)
	case strings.TrimSpace(req.Reason) == "":
		return fmt.Errorf("%w: reason is required", ErrInvalidRequest)
	case req.RequestedAt.IsZero():
		return fmt.Errorf("%w: requested time is required", ErrInvalidRequest)
	}
	return nil
}

func lock(ctx context.Context, tx dbport.Tx, req Request) error {
	key := "transaction.commit.boundary:" + req.Tenant.String() + ":" + req.PlanID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return fmt.Errorf("transaction cancellation: lock %s: %w", req.PlanID, err)
	}
	return nil
}

func plannedIdentity(req Request) string {
	return uuid.NewSHA1(identityNamespace, []byte(req.Tenant.String()+":"+req.PlanID+":"+req.PlanDigest)).String()
}

func findEvidence(ctx context.Context, q dbport.Querier, req Request, boundary string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT evidence_id
		FROM control_evidence
		WHERE tenant_id = $1 AND control_key = $2
		  AND scope->>'plan_id' = $3 AND scope->>'boundary' = $4
		ORDER BY collected_at DESC, evidence_id DESC LIMIT 1`, req.Tenant, evidenceControlKey, req.PlanID, boundary).Scan(&id)
	if errors.Is(err, dbport.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("transaction cancellation: read %s evidence: %w", boundary, err)
	}
	return id, true, nil
}

func committedIdentity(ctx context.Context, q dbport.Querier, req Request) (string, bool, error) {
	if id, found, err := findEvidenceIdentity(ctx, q, req, BoundaryAfterCommit); err != nil || found {
		return id, found, err
	}
	if id, err := uuid.Parse(req.PlanID); err == nil {
		var receipt uuid.UUID
		err := q.QueryRow(ctx, `SELECT receipt_id FROM transaction_commit_receipt WHERE tenant_id = $1 AND plan_id = $2`, req.Tenant, id).Scan(&receipt)
		if err == nil {
			return receipt.String(), true, nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return "", false, fmt.Errorf("transaction cancellation: read commit receipt: %w", err)
		}
	}
	if req.IdempotencyKey != "" {
		var status string
		var evidenceID *string
		err := q.QueryRow(ctx, `
			SELECT status, evidence_id FROM idempotency_record
			WHERE tenant_id = $1 AND capability_id = 'transaction.commit'
			  AND effect_scope = $2 AND idempotency_key = $3`, req.Tenant, req.PlanID, req.IdempotencyKey).Scan(&status, &evidenceID)
		if err == nil && status == "COMPLETED" {
			if evidenceID != nil && *evidenceID != "" {
				return *evidenceID, true, nil
			}
			return plannedIdentity(req), true, nil
		}
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return "", false, fmt.Errorf("transaction cancellation: read commit idempotency: %w", err)
		}
	}
	var count int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND source_ref LIKE $2`, req.Tenant, "transaction-plan:"+req.PlanID+":%").Scan(&count); err != nil {
		return "", false, fmt.Errorf("transaction cancellation: read committed ledger evidence: %w", err)
	}
	return plannedIdentity(req), count > 0, nil
}

func findEvidenceIdentity(ctx context.Context, q dbport.Querier, req Request, boundary string) (string, bool, error) {
	var identity string
	err := q.QueryRow(ctx, `
		SELECT scope->>'commit_identity'
		FROM control_evidence
		WHERE tenant_id = $1 AND control_key = $2
		  AND scope->>'plan_id' = $3 AND scope->>'boundary' = $4
		ORDER BY collected_at DESC, evidence_id DESC LIMIT 1`, req.Tenant, evidenceControlKey, req.PlanID, boundary).Scan(&identity)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("transaction cancellation: read %s identity: %w", boundary, err)
	}
	return identity, true, nil
}

func recordEvidence(ctx context.Context, tx dbport.Tx, req Request, boundary, identity string) (uuid.UUID, error) {
	scope := map[string]string{
		"boundary": boundary, "commit_identity": identity, "plan_id": req.PlanID,
		"plan_digest": req.PlanDigest, "reason": req.Reason, "requested_by": req.RequestedBy,
	}
	payload, err := json.Marshal(scope)
	if err != nil {
		return uuid.Nil, fmt.Errorf("transaction cancellation: encode evidence: %w", err)
	}
	sum := sha256.Sum256(payload)
	evidenceID := uuid.NewSHA1(identityNamespace, []byte(req.Tenant.String()+":"+req.PlanID+":"+boundary+":"+identity))
	at := req.RequestedAt.UTC()
	// The two boundaries may be requested at the same caller instant. Keep
	// their control-evidence windows distinct because the schema also has a
	// uniqueness fence on (control_key, control_version, window_start).
	if boundary == BoundaryAfterCommit {
		at = at.Add(time.Microsecond)
	}
	clock := at.Add(time.Hour)
	_, err = tx.Exec(ctx, `
		INSERT INTO control_evidence (
			tenant_id, evidence_id, control_key, control_version, implementation_ref,
			scope, window_start, window_end, source_ref, artifact_digest, collector,
			completeness, result, deficiency_ref, freshness_deadline, retention_class, collected_at)
		VALUES ($1, $2, $3, 1, 'internal/transaction/cancel', $4, $5, $6, $7, $8,
			$9, 'COMPLETE', 'PASS', '', $10, 'PERMANENT', $5)
		ON CONFLICT (tenant_id, evidence_id) DO NOTHING`, req.Tenant, evidenceID, evidenceControlKey,
		payload, at, at.Add(time.Microsecond), "transaction-plan:"+req.PlanID,
		hex.EncodeToString(sum[:]), req.RequestedBy, clock)
	if err != nil {
		return uuid.Nil, fmt.Errorf("transaction cancellation: record %s evidence: %w", boundary, err)
	}
	return evidenceID, nil
}

// recordAbortReceipt mirrors the cancellation into TX-005's typed abort
// receipt when this plan has been registered. Storage-neutral plans have no
// UUID registry row and retain control_evidence as their durable decision.
func recordAbortReceipt(ctx context.Context, tx dbport.Tx, req Request, evidence uuid.UUID, identity string) error {
	planID, err := uuid.Parse(req.PlanID)
	if err != nil {
		return nil
	}
	var intentDigest, proposalDigest, controlDigest, planDigest string
	err = tx.QueryRow(ctx, `SELECT intent_digest, proposal_digest, control_digest, plan_digest FROM transaction_plan WHERE tenant_id = $1 AND plan_id = $2`, req.Tenant, planID).Scan(&intentDigest, &proposalDigest, &controlDigest, &planDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("transaction cancellation: read registered plan: %w", err)
	}
	detail := "boundary=" + BoundaryBeforeCommit + ";commit_identity=" + identity
	_, err = tx.Exec(ctx, `
		INSERT INTO transaction_abort_receipt (
			tenant_id, receipt_id, plan_id, intent_digest, proposal_digest, control_digest,
			plan_digest, abort_reason_code, abort_detail, aborted_before_effects, aborted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'CANCELLED', $8, true, $9)
		ON CONFLICT (tenant_id, receipt_id) DO NOTHING`, req.Tenant, evidence, planID,
		intentDigest, proposalDigest, controlDigest, planDigest, detail, req.RequestedAt.UTC())
	if err != nil {
		return fmt.Errorf("transaction cancellation: record abort receipt: %w", err)
	}
	return nil
}

var identityNamespace = uuid.MustParse("2f71ef55-978c-4a55-846a-f44b5f3a8a04")

// Version is the package contract version used by architecture tooling.
func Version() int { return 1 }

// Explain returns a bounded operational description without request material.
func Explain() string {
	return "transaction.cancel v1: advisory-locked cancellation at the local commit boundary"
}
