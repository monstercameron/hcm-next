package intentcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Compiled plan statuses, matching internal/intent.PlanStatus and the schema.
const (
	PlanGovernanceValidated = "GOVERNANCE_VALIDATED"
	PlanBlocked             = "BLOCKED"
)

// Plan binding states, the coordinator's state machine from
// planning/specs/transaction-plan-and-commit-coordinator.md.
const (
	BindingDraft               = "DRAFT"
	BindingDomainValidated     = "DOMAIN_VALIDATED"
	BindingGovernanceValidated = "GOVERNANCE_VALIDATED"
	BindingApprovalBound       = "APPROVAL_BOUND"
	BindingReserved            = "RESERVED"
	BindingReady               = "READY"
	BindingCommitting          = "COMMITTING"
	BindingCommitted           = "COMMITTED"
	BindingStale               = "STALE"
	BindingAborted             = "ABORTED"
	BindingAmbiguous           = "AMBIGUOUS"
	BindingRepairRequired      = "REPAIR_REQUIRED"
)

// bindingTransitions is the coordinator's state machine as an explicit graph.
// The schema constrains which states exist; only this graph constrains which
// succession between two of them is legal, because a CHECK constraint sees the
// new row and never the old one.
var bindingTransitions = map[string][]string{
	BindingDraft:               {BindingDomainValidated, BindingStale, BindingAborted},
	BindingDomainValidated:     {BindingGovernanceValidated, BindingStale, BindingAborted},
	BindingGovernanceValidated: {BindingApprovalBound, BindingStale, BindingAborted},
	BindingApprovalBound:       {BindingReserved, BindingStale, BindingAborted},
	BindingReserved:            {BindingReady, BindingStale, BindingAborted},
	BindingReady:               {BindingCommitting, BindingStale, BindingAborted},
	BindingCommitting:          {BindingCommitted, BindingAborted, BindingAmbiguous},
	BindingAmbiguous:           {BindingCommitted, BindingAborted, BindingRepairRequired},
	BindingCommitted:           {BindingRepairRequired},
	BindingRepairRequired:      {BindingCommitted, BindingAborted},
	BindingStale:               {BindingAborted},
	BindingAborted:             nil,
}

// Plan is one transaction_plan row: an immutable compiled plan. The spec says
// the plan is never mutated in place, so there is no Update here -- a plan whose
// material context changed is a new row with a new digest.
type Plan struct {
	TenantID uuid.UUID
	PlanID   uuid.UUID
	IntentID uuid.UUID
	Revision uint64

	PlanDigest     string
	IntentDigest   string
	ProposalDigest string
	ControlDigest  string

	ExecutionMode  string
	CompiledStatus string

	GovernanceSnapshotDigest string
	ConflictSnapshotDigest   string
	ConflictFenceToken       string
	IdempotencyRecordRef     string

	EffectiveFrom time.Time
	EffectiveTo   time.Time
	ExpiresAt     time.Time

	CompiledBy string
	CompiledAt time.Time
	RecordedAt time.Time
}

// Validate rejects a plan that could not be stored, including the three digests
// the RED clause forbids losing.
func (p Plan) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", p.TenantID},
		{"plan_id", p.PlanID},
		{"intent_id", p.IntentID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	if p.Revision == 0 {
		return invalid("revision", "a proposal revision starts at 1")
	}
	for _, req := range []struct{ field, value string }{
		{"plan_digest", p.PlanDigest},
		{"intent_digest", p.IntentDigest},
		{"proposal_digest", p.ProposalDigest},
		{"control_digest", p.ControlDigest},
		{"governance_snapshot_digest", p.GovernanceSnapshotDigest},
		{"conflict_snapshot_digest", p.ConflictSnapshotDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"execution_mode", p.ExecutionMode},
		{"conflict_fence_token", p.ConflictFenceToken},
		{"idempotency_record_ref", p.IdempotencyRecordRef},
		{"compiled_by", p.CompiledBy},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	switch p.CompiledStatus {
	case PlanGovernanceValidated, PlanBlocked:
	default:
		return invalid("compiled_status", "status is not GOVERNANCE_VALIDATED or BLOCKED")
	}
	if err := requireInstant("effective_from", p.EffectiveFrom); err != nil {
		return err
	}
	if !p.EffectiveTo.IsZero() && !p.EffectiveTo.After(p.EffectiveFrom) {
		return invalid("effective_to", "business validity must be a half-open interval [from, to)")
	}
	if err := requireInstant("compiled_at", p.CompiledAt); err != nil {
		return err
	}
	if !p.ExpiresAt.After(p.CompiledAt) {
		return invalid("expires_at", "a plan must expire after it was compiled")
	}
	return nil
}

// PlanEffect is one transaction_plan_effect row.
type PlanEffect struct {
	EffectID             string
	DestinationRef       string
	IdempotencyKey       string
	Reversibility        string
	CompensationStrategy string
	RepairPlanRef        string
	ObservationRef       string
	ObservationDeadline  time.Time
}

// Validate rejects a plan effect that could not be stored. Compensation,
// repair, observation and deadline are all required, which is
// internal/intent.CompilePlan's own refusal made structural.
func (e PlanEffect) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"effect_id", e.EffectID},
		{"destination_ref", e.DestinationRef},
		{"effect_idempotency_key", e.IdempotencyKey},
		{"compensation_strategy", e.CompensationStrategy},
		{"repair_plan_ref", e.RepairPlanRef},
		{"observation_ref", e.ObservationRef},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	switch e.Reversibility {
	case Reversible, Compensatable, Irreversible:
	default:
		return invalid("reversibility", "value is not REVERSIBLE, COMPENSATABLE or IRREVERSIBLE")
	}
	return requireInstant("observation_deadline", e.ObservationDeadline)
}

// PlanStore writes and reads transaction_plan and transaction_plan_effect.
type PlanStore struct{}

// Compile inserts one immutable plan together with its declared effects and
// opens its binding at DRAFT.
//
// The three writes are one unit of meaning: a plan row with no binding could
// never advance, and a binding with no effects would claim a plan declares
// nothing when it declares several. They are not wrapped in a transaction here
// because the caller already owns one -- that is the whole reason [Executor] is
// a parameter -- so a failure part-way through disappears when the caller rolls
// back.
func (s PlanStore) Compile(ctx context.Context, ex Executor, plan Plan, effects []PlanEffect) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	for i, e := range effects {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("effects[%d]: %w", i, err)
		}
	}

	affected, err := ex.Exec(ctx, `
		INSERT INTO transaction_plan (
			tenant_id, plan_id, intent_id, revision,
			plan_digest, intent_digest, proposal_digest, control_digest,
			execution_mode, compiled_status,
			governance_snapshot_digest, conflict_snapshot_digest,
			conflict_fence_token, idempotency_record_ref,
			effective_from, effective_to, expires_at,
			compiled_by, compiled_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT DO NOTHING`,
		plan.TenantID, plan.PlanID, plan.IntentID, int64(plan.Revision),
		plan.PlanDigest, plan.IntentDigest, plan.ProposalDigest, plan.ControlDigest,
		plan.ExecutionMode, plan.CompiledStatus,
		plan.GovernanceSnapshotDigest, plan.ConflictSnapshotDigest,
		plan.ConflictFenceToken, plan.IdempotencyRecordRef,
		plan.EffectiveFrom.UTC(), nullableInstant(plan.EffectiveTo), plan.ExpiresAt.UTC(),
		plan.CompiledBy, plan.CompiledAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: compile plan %s: %w", plan.PlanID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: transaction_plan %s", ErrDuplicate, plan.PlanID)
	}

	for i, e := range effects {
		affected, err := ex.Exec(ctx, `
			INSERT INTO transaction_plan_effect (
				tenant_id, plan_id, effect_id, ordinal,
				destination_ref, effect_idempotency_key, reversibility,
				compensation_strategy, repair_plan_ref,
				observation_ref, observation_deadline)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT DO NOTHING`,
			plan.TenantID, plan.PlanID, e.EffectID, i+1,
			e.DestinationRef, e.IdempotencyKey, e.Reversibility,
			e.CompensationStrategy, e.RepairPlanRef,
			e.ObservationRef, e.ObservationDeadline.UTC())
		if err != nil {
			return fmt.Errorf("intentcontrol: record plan effect %s: %w", e.EffectID, err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: transaction_plan_effect %s/%s", ErrDuplicate, plan.PlanID, e.EffectID)
		}
	}

	if _, err := ex.Exec(ctx, `
		INSERT INTO transaction_plan_binding (tenant_id, plan_id, plan_state, binding_version, last_transition_at)
		VALUES ($1, $2, $3, 1, $4)`,
		plan.TenantID, plan.PlanID, BindingDraft, plan.CompiledAt.UTC()); err != nil {
		return fmt.Errorf("intentcontrol: open plan binding %s: %w", plan.PlanID, err)
	}
	return nil
}

// Load returns one plan.
func (s PlanStore) Load(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) (Plan, error) {
	var (
		out         Plan
		revision    int64
		effectiveTo *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, plan_id, intent_id, revision,
			plan_digest, intent_digest, proposal_digest, control_digest,
			execution_mode, compiled_status,
			governance_snapshot_digest, conflict_snapshot_digest,
			conflict_fence_token, idempotency_record_ref,
			effective_from, effective_to, expires_at,
			compiled_by, compiled_at, recorded_at
		FROM transaction_plan
		WHERE tenant_id = $1 AND plan_id = $2`, tenantID, planID).Scan(
		&out.TenantID, &out.PlanID, &out.IntentID, &revision,
		&out.PlanDigest, &out.IntentDigest, &out.ProposalDigest, &out.ControlDigest,
		&out.ExecutionMode, &out.CompiledStatus,
		&out.GovernanceSnapshotDigest, &out.ConflictSnapshotDigest,
		&out.ConflictFenceToken, &out.IdempotencyRecordRef,
		&out.EffectiveFrom, &effectiveTo, &out.ExpiresAt,
		&out.CompiledBy, &out.CompiledAt, &out.RecordedAt)
	if err != nil {
		if isNoRows(err) {
			return Plan{}, fmt.Errorf("%w: transaction_plan %s", ErrNotFound, planID)
		}
		return Plan{}, fmt.Errorf("intentcontrol: load plan %s: %w", planID, err)
	}
	out.Revision = uint64(revision)
	if effectiveTo != nil {
		out.EffectiveTo = effectiveTo.UTC()
	}
	out.EffectiveFrom = out.EffectiveFrom.UTC()
	out.ExpiresAt = out.ExpiresAt.UTC()
	out.CompiledAt = out.CompiledAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}

// Effects returns a plan's declared effects in ordinal order.
func (s PlanStore) Effects(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) ([]PlanEffect, error) {
	rows, err := ex.Query(ctx, `
		SELECT effect_id, destination_ref, effect_idempotency_key, reversibility,
			compensation_strategy, repair_plan_ref, observation_ref, observation_deadline
		FROM transaction_plan_effect
		WHERE tenant_id = $1 AND plan_id = $2
		ORDER BY ordinal`, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("intentcontrol: load plan effects %s: %w", planID, err)
	}
	defer rows.Close()

	var out []PlanEffect
	for rows.Next() {
		var e PlanEffect
		if err := rows.Scan(&e.EffectID, &e.DestinationRef, &e.IdempotencyKey, &e.Reversibility,
			&e.CompensationStrategy, &e.RepairPlanRef, &e.ObservationRef, &e.ObservationDeadline); err != nil {
			return nil, fmt.Errorf("intentcontrol: scan plan effect: %w", err)
		}
		e.ObservationDeadline = e.ObservationDeadline.UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intentcontrol: load plan effects %s: %w", planID, err)
	}
	return out, nil
}

// PlanBinding is one transaction_plan_binding row: where a plan currently sits
// on the coordinator's state machine.
type PlanBinding struct {
	TenantID uuid.UUID
	PlanID   uuid.UUID

	State          string
	BindingVersion uint64
	ApprovalDigest string

	LastTransitionAt time.Time
	RecordedAt       time.Time
}

// PlanBindingStore reads and advances transaction_plan_binding.
type PlanBindingStore struct{}

// Transition advances a plan binding under compare-and-swap.
//
// approvalDigest may be empty for a state that does not require one; it is
// required from APPROVAL_BOUND onwards, which the schema also checks. An
// illegal succession is [ErrIllegalTransition] and is refused before any
// statement runs; a stale expectedVersion is [ErrVersionConflict] and is
// refused by the UPDATE's own WHERE clause, so the compare and the swap are one
// statement with no window between them.
func (s PlanBindingStore) Transition(ctx context.Context, ex Executor, tenantID, planID uuid.UUID,
	expectedVersion uint64, next, approvalDigest string, at time.Time,
) (PlanBinding, error) {
	if expectedVersion == 0 {
		return PlanBinding{}, invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if err := requireInstant("last_transition_at", at); err != nil {
		return PlanBinding{}, err
	}
	if approvalDigest != "" {
		if err := requireDigest("approval_binding_digest", approvalDigest); err != nil {
			return PlanBinding{}, err
		}
	}
	current, err := s.Load(ctx, ex, tenantID, planID)
	if err != nil {
		return PlanBinding{}, err
	}
	if !allows(bindingTransitions, current.State, next) {
		return PlanBinding{}, fmt.Errorf("%w: transaction_plan_binding %s: %s -> %s",
			ErrIllegalTransition, planID, current.State, next)
	}
	// The approval binding, once set, is the exact material context the approval
	// was taken against. Carrying it forward rather than letting a caller pass ""
	// is what stops a later transition from silently unbinding it.
	if approvalDigest == "" {
		approvalDigest = current.ApprovalDigest
	}

	row := ex.QueryRow(ctx, `
		UPDATE transaction_plan_binding
		SET plan_state = $4,
			binding_version = binding_version + 1,
			approval_binding_digest = NULLIF($5, ''),
			last_transition_at = $6
		WHERE tenant_id = $1 AND plan_id = $2 AND binding_version = $3
		RETURNING tenant_id, plan_id, plan_state, binding_version,
			approval_binding_digest, last_transition_at, recorded_at`,
		tenantID, planID, int64(expectedVersion), next, approvalDigest, at.UTC())

	stored, err := scanBinding(row)
	if err != nil {
		if isNoRows(err) {
			return PlanBinding{}, fmt.Errorf("%w: transaction_plan_binding %s expected version %d",
				ErrVersionConflict, planID, expectedVersion)
		}
		return PlanBinding{}, fmt.Errorf("intentcontrol: transition plan binding %s: %w", planID, err)
	}
	return stored, nil
}

// Load returns one plan binding.
func (s PlanBindingStore) Load(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) (PlanBinding, error) {
	row := ex.QueryRow(ctx, `
		SELECT tenant_id, plan_id, plan_state, binding_version,
			approval_binding_digest, last_transition_at, recorded_at
		FROM transaction_plan_binding
		WHERE tenant_id = $1 AND plan_id = $2`, tenantID, planID)
	stored, err := scanBinding(row)
	if err != nil {
		if isNoRows(err) {
			return PlanBinding{}, fmt.Errorf("%w: transaction_plan_binding %s", ErrNotFound, planID)
		}
		return PlanBinding{}, fmt.Errorf("intentcontrol: load plan binding %s: %w", planID, err)
	}
	return stored, nil
}

func scanBinding(src scanner) (PlanBinding, error) {
	var (
		out      PlanBinding
		version  int64
		approval *string
	)
	if err := src.Scan(&out.TenantID, &out.PlanID, &out.State, &version,
		&approval, &out.LastTransitionAt, &out.RecordedAt); err != nil {
		return PlanBinding{}, err
	}
	out.BindingVersion = uint64(version)
	out.ApprovalDigest = derefString(approval)
	out.LastTransitionAt = out.LastTransitionAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}

// CommitReceipt is one transaction_commit_receipt row.
type CommitReceipt struct {
	TenantID  uuid.UUID
	ReceiptID uuid.UUID
	PlanID    uuid.UUID

	IntentDigest   string
	ProposalDigest string
	ControlDigest  string
	PlanDigest     string

	DatabaseTransactionID string
	IdempotencyRecordRef  string

	AppendedEventCount      int
	ProjectionMutationCount int
	OutboxEffectCount       int
	ProducedReferenceDigest string

	CommittedAt time.Time
	RecordedAt  time.Time
}

// Validate rejects a commit receipt that could not be stored.
func (r CommitReceipt) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", r.TenantID},
		{"receipt_id", r.ReceiptID},
		{"plan_id", r.PlanID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"intent_digest", r.IntentDigest},
		{"proposal_digest", r.ProposalDigest},
		{"control_digest", r.ControlDigest},
		{"plan_digest", r.PlanDigest},
		{"produced_reference_digest", r.ProducedReferenceDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"database_transaction_id", r.DatabaseTransactionID},
		{"idempotency_record_ref", r.IdempotencyRecordRef},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	if r.AppendedEventCount < 0 || r.ProjectionMutationCount < 0 || r.OutboxEffectCount < 0 {
		return invalid("counts", "a commit cannot produce a negative number of anything")
	}
	return requireInstant("committed_at", r.CommittedAt)
}

// AbortReceipt is one transaction_abort_receipt row.
type AbortReceipt struct {
	TenantID  uuid.UUID
	ReceiptID uuid.UUID
	PlanID    uuid.UUID

	IntentDigest   string
	ProposalDigest string
	ControlDigest  string
	PlanDigest     string

	ReasonCode           string
	Detail               string
	AbortedBeforeEffects bool

	AbortedAt  time.Time
	RecordedAt time.Time
}

// Abort reason codes, matching the schema's closed vocabulary and the spec's
// typed failure list.
const (
	AbortPlanExpired         = "PLAN_EXPIRED"
	AbortPlanStale           = "PLAN_STALE"
	AbortDigestMismatch      = "DIGEST_MISMATCH"
	AbortApprovalMismatch    = "APPROVAL_MISMATCH"
	AbortDomainRejected      = "DOMAIN_REJECTED"
	AbortGovernanceRejected  = "GOVERNANCE_REJECTED"
	AbortSequenceConflict    = "SEQUENCE_CONFLICT"
	AbortReservationLost     = "RESERVATION_LOST"
	AbortAuthorityChanged    = "AUTHORITY_CHANGED"
	AbortIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	AbortDatabaseAbort       = "DATABASE_ABORT"
	AbortCancelled           = "CANCELLED"
)

var abortReasons = map[string]bool{
	AbortPlanExpired: true, AbortPlanStale: true, AbortDigestMismatch: true,
	AbortApprovalMismatch: true, AbortDomainRejected: true, AbortGovernanceRejected: true,
	AbortSequenceConflict: true, AbortReservationLost: true, AbortAuthorityChanged: true,
	AbortIdempotencyConflict: true, AbortDatabaseAbort: true, AbortCancelled: true,
}

// Validate rejects an abort receipt that could not be stored.
func (r AbortReceipt) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", r.TenantID},
		{"receipt_id", r.ReceiptID},
		{"plan_id", r.PlanID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"intent_digest", r.IntentDigest},
		{"proposal_digest", r.ProposalDigest},
		{"control_digest", r.ControlDigest},
		{"plan_digest", r.PlanDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	if !abortReasons[r.ReasonCode] {
		return invalid("abort_reason_code", "reason is not a declared abort reason")
	}
	if err := requireText("abort_detail", r.Detail); err != nil {
		return err
	}
	return requireInstant("aborted_at", r.AbortedAt)
}

// ReceiptStore writes and reads the two receipt tables.
type ReceiptStore struct{}

// lockPlan takes a transaction-scoped advisory lock on one plan, and is the
// serialization point the two receipt tables do not have between them.
//
// Each receipt table carries its own UNIQUE (tenant_id, plan_id), so neither
// kind can be recorded twice. What neither constraint can see is the other
// table: a plan that both committed and aborted is a contradiction spread over
// two rows, and under READ COMMITTED two concurrent transactions would each
// read "no counterpart" and each insert into a different table. The lock closes
// that window by making the counterpart check and the insert one critical
// section per plan; it is released when the caller's transaction ends, whether
// it commits or rolls back, so no caller has to remember to release it.
//
// The key is derived from tenant and plan rather than from the plan alone,
// because the advisory-lock space is global to the database while these rows
// are tenant-scoped: two tenants coordinating on the same random uuid would
// otherwise block each other for no reason.
func (s ReceiptStore) lockPlan(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) error {
	key := "transaction_receipt:" + tenantID.String() + ":" + planID.String()
	if _, err := ex.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return fmt.Errorf("intentcontrol: lock plan %s: %w", planID, err)
	}
	return nil
}

// RecordCommit inserts a commit receipt, refusing when the plan already holds
// an abort receipt.
//
// The counterpart check and the insert run in the caller's transaction behind
// [ReceiptStore.lockPlan], so a concurrent writer cannot slip between them --
// and the second writer's own insert would still collide on its table's unique
// constraint if it somehow did.
func (s ReceiptStore) RecordCommit(ctx context.Context, ex Executor, in CommitReceipt) error {
	if err := in.Validate(); err != nil {
		return err
	}
	if err := s.lockPlan(ctx, ex, in.TenantID, in.PlanID); err != nil {
		return err
	}
	aborted, err := s.hasAbort(ctx, ex, in.TenantID, in.PlanID)
	if err != nil {
		return err
	}
	if aborted {
		return fmt.Errorf("%w: plan %s already holds an abort receipt", ErrReceiptConflict, in.PlanID)
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO transaction_commit_receipt (
			tenant_id, receipt_id, plan_id,
			intent_digest, proposal_digest, control_digest, plan_digest,
			database_transaction_id, idempotency_record_ref,
			appended_event_count, projection_mutation_count, outbox_effect_count,
			produced_reference_digest, committed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.ReceiptID, in.PlanID,
		in.IntentDigest, in.ProposalDigest, in.ControlDigest, in.PlanDigest,
		in.DatabaseTransactionID, in.IdempotencyRecordRef,
		in.AppendedEventCount, in.ProjectionMutationCount, in.OutboxEffectCount,
		in.ProducedReferenceDigest, in.CommittedAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: record commit receipt %s: %w", in.ReceiptID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: transaction_commit_receipt for plan %s", ErrDuplicate, in.PlanID)
	}
	return nil
}

// RecordAbort inserts an abort receipt, refusing when the plan already holds a
// commit receipt. It is the mirror of [ReceiptStore.RecordCommit].
func (s ReceiptStore) RecordAbort(ctx context.Context, ex Executor, in AbortReceipt) error {
	if err := in.Validate(); err != nil {
		return err
	}
	if err := s.lockPlan(ctx, ex, in.TenantID, in.PlanID); err != nil {
		return err
	}
	committed, err := s.hasCommit(ctx, ex, in.TenantID, in.PlanID)
	if err != nil {
		return err
	}
	if committed {
		return fmt.Errorf("%w: plan %s already holds a commit receipt", ErrReceiptConflict, in.PlanID)
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO transaction_abort_receipt (
			tenant_id, receipt_id, plan_id,
			intent_digest, proposal_digest, control_digest, plan_digest,
			abort_reason_code, abort_detail, aborted_before_effects, aborted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.ReceiptID, in.PlanID,
		in.IntentDigest, in.ProposalDigest, in.ControlDigest, in.PlanDigest,
		in.ReasonCode, in.Detail, in.AbortedBeforeEffects, in.AbortedAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: record abort receipt %s: %w", in.ReceiptID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: transaction_abort_receipt for plan %s", ErrDuplicate, in.PlanID)
	}
	return nil
}

// LoadCommit returns the commit receipt of one plan.
func (s ReceiptStore) LoadCommit(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) (CommitReceipt, error) {
	var out CommitReceipt
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, receipt_id, plan_id,
			intent_digest, proposal_digest, control_digest, plan_digest,
			database_transaction_id, idempotency_record_ref,
			appended_event_count, projection_mutation_count, outbox_effect_count,
			produced_reference_digest, committed_at, recorded_at
		FROM transaction_commit_receipt
		WHERE tenant_id = $1 AND plan_id = $2`, tenantID, planID).Scan(
		&out.TenantID, &out.ReceiptID, &out.PlanID,
		&out.IntentDigest, &out.ProposalDigest, &out.ControlDigest, &out.PlanDigest,
		&out.DatabaseTransactionID, &out.IdempotencyRecordRef,
		&out.AppendedEventCount, &out.ProjectionMutationCount, &out.OutboxEffectCount,
		&out.ProducedReferenceDigest, &out.CommittedAt, &out.RecordedAt)
	if err != nil {
		if isNoRows(err) {
			return CommitReceipt{}, fmt.Errorf("%w: transaction_commit_receipt for plan %s", ErrNotFound, planID)
		}
		return CommitReceipt{}, fmt.Errorf("intentcontrol: load commit receipt %s: %w", planID, err)
	}
	out.CommittedAt = out.CommittedAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}

func (s ReceiptStore) hasCommit(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) (bool, error) {
	return s.exists(ctx, ex, `SELECT 1 FROM transaction_commit_receipt WHERE tenant_id = $1 AND plan_id = $2`,
		tenantID, planID, "commit")
}

func (s ReceiptStore) hasAbort(ctx context.Context, ex Executor, tenantID, planID uuid.UUID) (bool, error) {
	return s.exists(ctx, ex, `SELECT 1 FROM transaction_abort_receipt WHERE tenant_id = $1 AND plan_id = $2`,
		tenantID, planID, "abort")
}

func (s ReceiptStore) exists(ctx context.Context, ex Executor, query string, tenantID, planID uuid.UUID, kind string) (bool, error) {
	var one int
	err := ex.QueryRow(ctx, query, tenantID, planID).Scan(&one)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, fmt.Errorf("intentcontrol: check %s receipt of plan %s: %w", kind, planID, err)
	}
	return true, nil
}

// Ambiguity resolution states and outcomes.
const (
	AmbiguityUnresolved = "UNRESOLVED"
	AmbiguityResolved   = "RESOLVED"

	AmbiguityOutcomeCommitted      = "COMMITTED"
	AmbiguityOutcomeAborted        = "ABORTED"
	AmbiguityOutcomeRepairRequired = "REPAIR_REQUIRED"
)

// Ambiguity is one transaction_ambiguity row: a commit whose outcome the
// coordinator could not observe. It is live state -- it is opened UNRESOLVED and
// later resolved -- so it carries a compare-and-swap version.
type Ambiguity struct {
	TenantID    uuid.UUID
	AmbiguityID uuid.UUID
	PlanID      uuid.UUID

	DetectedAt time.Time
	Detail     string

	ResolutionState string
	Outcome         string
	ResolvedAt      time.Time
	EvidenceRef     string

	Version    uint64
	RecordedAt time.Time
}

// AmbiguityStore writes and resolves transaction_ambiguity.
type AmbiguityStore struct{}

// Open records a detected ambiguity in the UNRESOLVED state.
func (s AmbiguityStore) Open(ctx context.Context, ex Executor, in Ambiguity) error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", in.TenantID},
		{"ambiguity_id", in.AmbiguityID},
		{"plan_id", in.PlanID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireText("detection_detail", in.Detail); err != nil {
		return err
	}
	if err := requireInstant("detected_at", in.DetectedAt); err != nil {
		return err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO transaction_ambiguity (
			tenant_id, ambiguity_id, plan_id, detected_at, detection_detail,
			resolution_state, ambiguity_version)
		VALUES ($1, $2, $3, $4, $5, $6, 1)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.AmbiguityID, in.PlanID, in.DetectedAt.UTC(), in.Detail, AmbiguityUnresolved)
	if err != nil {
		return fmt.Errorf("intentcontrol: open ambiguity %s: %w", in.AmbiguityID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: transaction_ambiguity for plan %s", ErrDuplicate, in.PlanID)
	}
	return nil
}

// Resolve closes an ambiguity under compare-and-swap, recording the outcome and
// the evidence it was resolved on.
//
// The evidence reference is required: the spec says the coordinator queries the
// transaction or idempotency receipt before any retry, so a resolution that
// names no evidence is a guess, and this store does not record guesses. An
// already-resolved ambiguity is [ErrIllegalTransition] -- reopening a resolved
// outcome would erase the evidence the first resolution was made on.
func (s AmbiguityStore) Resolve(ctx context.Context, ex Executor, tenantID, ambiguityID uuid.UUID,
	expectedVersion uint64, outcome, evidenceRef string, at time.Time,
) (Ambiguity, error) {
	if expectedVersion == 0 {
		return Ambiguity{}, invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	switch outcome {
	case AmbiguityOutcomeCommitted, AmbiguityOutcomeAborted, AmbiguityOutcomeRepairRequired:
	default:
		return Ambiguity{}, invalid("resolved_outcome", "outcome is not a declared ambiguity outcome")
	}
	if err := requireText("resolution_evidence_ref", evidenceRef); err != nil {
		return Ambiguity{}, err
	}
	if err := requireInstant("resolved_at", at); err != nil {
		return Ambiguity{}, err
	}
	current, err := s.Load(ctx, ex, tenantID, ambiguityID)
	if err != nil {
		return Ambiguity{}, err
	}
	if current.ResolutionState == AmbiguityResolved {
		return Ambiguity{}, fmt.Errorf("%w: transaction_ambiguity %s is already resolved as %s",
			ErrIllegalTransition, ambiguityID, current.Outcome)
	}
	if at.Before(current.DetectedAt) {
		return Ambiguity{}, invalid("resolved_at", "an ambiguity cannot be resolved before it was detected")
	}

	row := ex.QueryRow(ctx, `
		UPDATE transaction_ambiguity
		SET resolution_state = $4,
			resolved_outcome = $5,
			resolved_at = $6,
			resolution_evidence_ref = $7,
			ambiguity_version = ambiguity_version + 1
		WHERE tenant_id = $1 AND ambiguity_id = $2 AND ambiguity_version = $3
		RETURNING `+ambiguityColumns,
		tenantID, ambiguityID, int64(expectedVersion),
		AmbiguityResolved, outcome, at.UTC(), evidenceRef)

	stored, err := scanAmbiguity(row)
	if err != nil {
		if isNoRows(err) {
			return Ambiguity{}, fmt.Errorf("%w: transaction_ambiguity %s expected version %d",
				ErrVersionConflict, ambiguityID, expectedVersion)
		}
		return Ambiguity{}, fmt.Errorf("intentcontrol: resolve ambiguity %s: %w", ambiguityID, err)
	}
	return stored, nil
}

// Load returns one ambiguity record.
func (s AmbiguityStore) Load(ctx context.Context, ex Executor, tenantID, ambiguityID uuid.UUID) (Ambiguity, error) {
	row := ex.QueryRow(ctx, `
		SELECT `+ambiguityColumns+`
		FROM transaction_ambiguity
		WHERE tenant_id = $1 AND ambiguity_id = $2`, tenantID, ambiguityID)
	stored, err := scanAmbiguity(row)
	if err != nil {
		if isNoRows(err) {
			return Ambiguity{}, fmt.Errorf("%w: transaction_ambiguity %s", ErrNotFound, ambiguityID)
		}
		return Ambiguity{}, fmt.Errorf("intentcontrol: load ambiguity %s: %w", ambiguityID, err)
	}
	return stored, nil
}

const ambiguityColumns = `tenant_id, ambiguity_id, plan_id, detected_at, detection_detail,
	resolution_state, resolved_outcome, resolved_at, resolution_evidence_ref,
	ambiguity_version, recorded_at`

func scanAmbiguity(src scanner) (Ambiguity, error) {
	var (
		out        Ambiguity
		outcome    *string
		resolvedAt *time.Time
		evidence   *string
		version    int64
	)
	if err := src.Scan(&out.TenantID, &out.AmbiguityID, &out.PlanID, &out.DetectedAt, &out.Detail,
		&out.ResolutionState, &outcome, &resolvedAt, &evidence, &version, &out.RecordedAt); err != nil {
		return Ambiguity{}, err
	}
	out.Outcome = derefString(outcome)
	out.EvidenceRef = derefString(evidence)
	if resolvedAt != nil {
		out.ResolvedAt = resolvedAt.UTC()
	}
	out.Version = uint64(version)
	out.DetectedAt = out.DetectedAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}

// Correction is one transaction_correction row. It always names the commit
// receipt it corrects; there is no shape of this struct that does not.
type Correction struct {
	TenantID          uuid.UUID
	CorrectionID      uuid.UUID
	CorrectsReceiptID uuid.UUID
	CorrectingIntent  uuid.UUID

	IntentDigest   string
	ProposalDigest string
	ControlDigest  string

	Reason      string
	CorrectedBy string
	CorrectedAt time.Time
	RecordedAt  time.Time
}

// Validate rejects a correction that could not be stored, starting with the
// target the RED clause says a correction must never lack.
func (c Correction) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", c.TenantID},
		{"correction_id", c.CorrectionID},
		{"corrects_receipt_id", c.CorrectsReceiptID},
		{"correcting_intent_id", c.CorrectingIntent},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"intent_digest", c.IntentDigest},
		{"proposal_digest", c.ProposalDigest},
		{"control_digest", c.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"correction_reason", c.Reason},
		{"corrected_by", c.CorrectedBy},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	return requireInstant("corrected_at", c.CorrectedAt)
}

// CorrectionStore writes transaction_correction.
type CorrectionStore struct{}

// Record inserts one correction.
func (s CorrectionStore) Record(ctx context.Context, ex Executor, in Correction) error {
	if err := in.Validate(); err != nil {
		return err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO transaction_correction (
			tenant_id, correction_id, corrects_receipt_id, correcting_intent_id,
			intent_digest, proposal_digest, control_digest,
			correction_reason, corrected_by, corrected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.CorrectionID, in.CorrectsReceiptID, in.CorrectingIntent,
		in.IntentDigest, in.ProposalDigest, in.ControlDigest,
		in.Reason, in.CorrectedBy, in.CorrectedAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: record correction %s: %w", in.CorrectionID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: transaction_correction %s", ErrDuplicate, in.CorrectionID)
	}
	return nil
}

// Repair statuses, matching the schema's closed vocabulary.
const (
	RepairOpen       = "OPEN"
	RepairInProgress = "IN_PROGRESS"
	RepairRepaired   = "REPAIRED"
	RepairAbandoned  = "ABANDONED"
)

var repairTransitions = map[string][]string{
	RepairOpen:       {RepairInProgress, RepairAbandoned},
	RepairInProgress: {RepairRepaired, RepairAbandoned},
	RepairRepaired:   nil,
	RepairAbandoned:  nil,
}

// RepairPlan is one repair_plan row: how a partially-applied or ambiguous
// transaction is to be put right. It is live state, so it carries a
// compare-and-swap version, and it still carries all three digests.
type RepairPlan struct {
	TenantID     uuid.UUID
	RepairPlanID uuid.UUID
	PlanID       uuid.UUID

	IntentDigest   string
	ProposalDigest string
	ControlDigest  string

	Status   string
	Strategy string
	Version  uint64

	SchemaRef string
	Body      json.RawMessage

	OpenedBy         string
	OpenedAt         time.Time
	LastTransitionAt time.Time
	RecordedAt       time.Time
}

// Validate rejects a repair plan that could not be stored.
func (r RepairPlan) Validate() error {
	for _, req := range []struct {
		field string
		value uuid.UUID
	}{
		{"tenant_id", r.TenantID},
		{"repair_plan_id", r.RepairPlanID},
		{"plan_id", r.PlanID},
	} {
		if err := requireID(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"intent_digest", r.IntentDigest},
		{"proposal_digest", r.ProposalDigest},
		{"control_digest", r.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	if _, known := repairTransitions[r.Status]; !known {
		return invalid("repair_status", "status is not a declared repair status")
	}
	for _, req := range []struct{ field, value string }{
		{"repair_strategy", r.Strategy},
		{"schema_ref", r.SchemaRef},
		{"opened_by", r.OpenedBy},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireInstant("opened_at", r.OpenedAt); err != nil {
		return err
	}
	return requireJSONObject("repair_body", r.Body)
}

// RepairPlanStore writes and advances repair_plan.
type RepairPlanStore struct{}

// Open records a repair plan in the OPEN state.
func (s RepairPlanStore) Open(ctx context.Context, ex Executor, in RepairPlan) error {
	if in.Status == "" {
		in.Status = RepairOpen
	}
	if in.LastTransitionAt.IsZero() {
		in.LastTransitionAt = in.OpenedAt
	}
	if err := in.Validate(); err != nil {
		return err
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO repair_plan (
			tenant_id, repair_plan_id, plan_id,
			intent_digest, proposal_digest, control_digest,
			repair_status, repair_strategy, repair_version,
			schema_ref, repair_body, opened_by, opened_at, last_transition_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $10::jsonb, $11, $12, $13)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.RepairPlanID, in.PlanID,
		in.IntentDigest, in.ProposalDigest, in.ControlDigest,
		in.Status, in.Strategy,
		in.SchemaRef, string(in.Body), in.OpenedBy, in.OpenedAt.UTC(), in.LastTransitionAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: open repair plan %s: %w", in.RepairPlanID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: repair_plan for plan %s", ErrDuplicate, in.PlanID)
	}
	return nil
}

// Transition advances a repair plan under compare-and-swap.
func (s RepairPlanStore) Transition(ctx context.Context, ex Executor, tenantID, repairPlanID uuid.UUID,
	expectedVersion uint64, next string, at time.Time,
) (RepairPlan, error) {
	if expectedVersion == 0 {
		return RepairPlan{}, invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if err := requireInstant("last_transition_at", at); err != nil {
		return RepairPlan{}, err
	}
	current, err := s.Load(ctx, ex, tenantID, repairPlanID)
	if err != nil {
		return RepairPlan{}, err
	}
	if !allows(repairTransitions, current.Status, next) {
		return RepairPlan{}, fmt.Errorf("%w: repair_plan %s: %s -> %s",
			ErrIllegalTransition, repairPlanID, current.Status, next)
	}

	row := ex.QueryRow(ctx, `
		UPDATE repair_plan
		SET repair_status = $4,
			repair_version = repair_version + 1,
			last_transition_at = $5
		WHERE tenant_id = $1 AND repair_plan_id = $2 AND repair_version = $3
		RETURNING `+repairColumns,
		tenantID, repairPlanID, int64(expectedVersion), next, at.UTC())

	stored, err := scanRepair(row)
	if err != nil {
		if isNoRows(err) {
			return RepairPlan{}, fmt.Errorf("%w: repair_plan %s expected version %d",
				ErrVersionConflict, repairPlanID, expectedVersion)
		}
		return RepairPlan{}, fmt.Errorf("intentcontrol: transition repair plan %s: %w", repairPlanID, err)
	}
	return stored, nil
}

// Load returns one repair plan.
func (s RepairPlanStore) Load(ctx context.Context, ex Executor, tenantID, repairPlanID uuid.UUID) (RepairPlan, error) {
	row := ex.QueryRow(ctx, `
		SELECT `+repairColumns+`
		FROM repair_plan
		WHERE tenant_id = $1 AND repair_plan_id = $2`, tenantID, repairPlanID)
	stored, err := scanRepair(row)
	if err != nil {
		if isNoRows(err) {
			return RepairPlan{}, fmt.Errorf("%w: repair_plan %s", ErrNotFound, repairPlanID)
		}
		return RepairPlan{}, fmt.Errorf("intentcontrol: load repair plan %s: %w", repairPlanID, err)
	}
	return stored, nil
}

const repairColumns = `tenant_id, repair_plan_id, plan_id,
	intent_digest, proposal_digest, control_digest,
	repair_status, repair_strategy, repair_version,
	schema_ref, repair_body::text, opened_by, opened_at, last_transition_at, recorded_at`

func scanRepair(src scanner) (RepairPlan, error) {
	var (
		out     RepairPlan
		version int64
		body    string
	)
	if err := src.Scan(&out.TenantID, &out.RepairPlanID, &out.PlanID,
		&out.IntentDigest, &out.ProposalDigest, &out.ControlDigest,
		&out.Status, &out.Strategy, &version,
		&out.SchemaRef, &body, &out.OpenedBy, &out.OpenedAt, &out.LastTransitionAt, &out.RecordedAt); err != nil {
		return RepairPlan{}, err
	}
	out.Version = uint64(version)
	out.Body = json.RawMessage(body)
	out.OpenedAt = out.OpenedAt.UTC()
	out.LastTransitionAt = out.LastTransitionAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}

// Closure kinds, matching the schema's closed vocabulary.
const (
	ClosureCommitted  = "COMMITTED"
	ClosureCorrected  = "CORRECTED"
	ClosureRejected   = "REJECTED"
	ClosureWithdrawn  = "WITHDRAWN"
	ClosureCancelled  = "CANCELLED"
	ClosureSuperseded = "SUPERSEDED"
	ClosureExpired    = "EXPIRED"
)

var closureKinds = map[string]bool{
	ClosureCommitted: true, ClosureCorrected: true, ClosureRejected: true,
	ClosureWithdrawn: true, ClosureCancelled: true, ClosureSuperseded: true,
	ClosureExpired: true,
}

// Closure is one intent_closure row: the terminal record for an intent and the
// execution receipt that explains it.
type Closure struct {
	TenantID uuid.UUID
	IntentID uuid.UUID

	Kind   string
	Reason string

	IntentDigest   string
	ProposalDigest string
	ControlDigest  string

	// ExecutionReceiptID is set for exactly the executed closure kinds
	// (COMMITTED, CORRECTED) and nil for the rest.
	ExecutionReceiptID uuid.UUID

	ClosedBy   string
	ClosedAt   time.Time
	RecordedAt time.Time
}

// executed reports whether this closure kind claims execution happened.
func (c Closure) executed() bool {
	return c.Kind == ClosureCommitted || c.Kind == ClosureCorrected
}

// Validate rejects a closure that could not be stored, including the receipt
// rule the schema also enforces: an executed closure names its receipt, and a
// never-executed one may not invent one.
func (c Closure) Validate() error {
	if err := requireID("tenant_id", c.TenantID); err != nil {
		return err
	}
	if err := requireID("intent_id", c.IntentID); err != nil {
		return err
	}
	if !closureKinds[c.Kind] {
		return invalid("closure_kind", "kind is not a declared closure kind")
	}
	for _, req := range []struct{ field, value string }{
		{"intent_digest", c.IntentDigest},
		{"proposal_digest", c.ProposalDigest},
		{"control_digest", c.ControlDigest},
	} {
		if err := requireDigest(req.field, req.value); err != nil {
			return err
		}
	}
	for _, req := range []struct{ field, value string }{
		{"closure_reason", c.Reason},
		{"closed_by", c.ClosedBy},
	} {
		if err := requireText(req.field, req.value); err != nil {
			return err
		}
	}
	if err := requireInstant("closed_at", c.ClosedAt); err != nil {
		return err
	}
	if c.executed() && c.ExecutionReceiptID == uuid.Nil {
		return invalid("execution_receipt_id", "a "+c.Kind+" closure must name the receipt that explains it")
	}
	if !c.executed() && c.ExecutionReceiptID != uuid.Nil {
		return invalid("execution_receipt_id", "a "+c.Kind+" closure executed nothing and has no receipt to name")
	}
	return nil
}

// ClosureStore writes and reads intent_closure.
type ClosureStore struct{}

// Close inserts the terminal closure row for one intent. An intent closes once:
// a second Close is [ErrDuplicate].
func (s ClosureStore) Close(ctx context.Context, ex Executor, in Closure) error {
	if err := in.Validate(); err != nil {
		return err
	}
	var receipt any
	if in.ExecutionReceiptID != uuid.Nil {
		receipt = in.ExecutionReceiptID
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO intent_closure (
			tenant_id, intent_id, closure_kind, closure_reason,
			intent_digest, proposal_digest, control_digest,
			execution_receipt_id, closed_by, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.IntentID, in.Kind, in.Reason,
		in.IntentDigest, in.ProposalDigest, in.ControlDigest,
		receipt, in.ClosedBy, in.ClosedAt.UTC())
	if err != nil {
		return fmt.Errorf("intentcontrol: close intent %s: %w", in.IntentID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: intent_closure %s", ErrDuplicate, in.IntentID)
	}
	return nil
}

// Load returns the closure of one intent.
func (s ClosureStore) Load(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID) (Closure, error) {
	var (
		out     Closure
		receipt *uuid.UUID
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, intent_id, closure_kind, closure_reason,
			intent_digest, proposal_digest, control_digest,
			execution_receipt_id, closed_by, closed_at, recorded_at
		FROM intent_closure
		WHERE tenant_id = $1 AND intent_id = $2`, tenantID, intentID).Scan(
		&out.TenantID, &out.IntentID, &out.Kind, &out.Reason,
		&out.IntentDigest, &out.ProposalDigest, &out.ControlDigest,
		&receipt, &out.ClosedBy, &out.ClosedAt, &out.RecordedAt)
	if err != nil {
		if isNoRows(err) {
			return Closure{}, fmt.Errorf("%w: intent_closure %s", ErrNotFound, intentID)
		}
		return Closure{}, fmt.Errorf("intentcontrol: load closure %s: %w", intentID, err)
	}
	if receipt != nil {
		out.ExecutionReceiptID = *receipt
	}
	out.ClosedAt = out.ClosedAt.UTC()
	out.RecordedAt = out.RecordedAt.UTC()
	return out, nil
}
