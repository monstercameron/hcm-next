package runtimestate

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Queue states.
const (
	QueueActive  = "ACTIVE"
	QueuePaused  = "PAUSED"
	QueueRetired = "RETIRED"
)

var queueTransitions = map[string][]string{
	QueueActive:  {QueuePaused, QueueRetired},
	QueuePaused:  {QueueActive, QueueRetired},
	QueueRetired: nil,
}

// Queue is one work_queue row: a named, governed destination for human work.
type Queue struct {
	TenantID uuid.UUID
	QueueID  uuid.UUID
	Key      string

	DisplayName      string
	State            string
	RoutingPolicyRef string
	Version          uint64

	CreatedAt time.Time
}

// QueueStore writes and advances work_queue and work_queue_item.
type QueueStore struct{}

// Create registers one queue. A repeated key is [ErrDuplicate]: a queue key is
// how routing names its destination, so two queues answering to one key would
// make routing ambiguous.
func (s QueueStore) Create(ctx context.Context, ex Executor, in Queue) error {
	if in.Key == "" || in.DisplayName == "" {
		return invalid("queue_key", "a queue has a key and a display name")
	}
	if in.RoutingPolicyRef == "" {
		return invalid("routing_policy_ref", "a queue names the policy that routes into it")
	}
	if in.CreatedAt.IsZero() {
		return invalid("created_at", "timestamp is unset")
	}
	if in.State == "" {
		in.State = QueueActive
	}
	if _, known := queueTransitions[in.State]; !known {
		return invalid("queue_state", "state is not a declared queue state")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO work_queue (
			tenant_id, queue_id, queue_key, display_name,
			queue_state, routing_policy_ref, queue_version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 1, $7)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.QueueID, in.Key, in.DisplayName,
		in.State, in.RoutingPolicyRef, in.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: create queue %s: %w", in.Key, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_queue %s", ErrDuplicate, in.Key)
	}
	return nil
}

// SetState advances a queue's state under compare-and-swap.
func (s QueueStore) SetState(ctx context.Context, ex Executor, tenantID, queueID uuid.UUID,
	expectedVersion uint64, next string,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	current, err := s.Load(ctx, ex, tenantID, queueID)
	if err != nil {
		return err
	}
	if !allows(queueTransitions, current.State, next) {
		return fmt.Errorf("%w: work_queue %s: %s -> %s", ErrIllegalTransition, queueID, current.State, next)
	}
	affected, err := ex.Exec(ctx, `
		UPDATE work_queue
		SET queue_state = $4, queue_version = queue_version + 1
		WHERE tenant_id = $1 AND queue_id = $2 AND queue_version = $3`,
		tenantID, queueID, int64(expectedVersion), next)
	if err != nil {
		return fmt.Errorf("runtimestate: set queue %s state: %w", queueID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_queue %s expected version %d", ErrVersionConflict, queueID, expectedVersion)
	}
	return nil
}

// Load returns one queue.
func (s QueueStore) Load(ctx context.Context, ex Executor, tenantID, queueID uuid.UUID) (Queue, error) {
	var (
		out     Queue
		version int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, queue_id, queue_key, display_name,
			queue_state, routing_policy_ref, queue_version, created_at
		FROM work_queue
		WHERE tenant_id = $1 AND queue_id = $2`, tenantID, queueID).Scan(
		&out.TenantID, &out.QueueID, &out.Key, &out.DisplayName,
		&out.State, &out.RoutingPolicyRef, &version, &out.CreatedAt)
	if err != nil {
		if isNoRows(err) {
			return Queue{}, fmt.Errorf("%w: work_queue %s", ErrNotFound, queueID)
		}
		return Queue{}, fmt.Errorf("runtimestate: load queue %s: %w", queueID, err)
	}
	out.Version = uint64(version)
	out.CreatedAt = out.CreatedAt.UTC()
	return out, nil
}

// Queue membership states.
const (
	MembershipQueued     = "QUEUED"
	MembershipDispatched = "DISPATCHED"
	MembershipRemoved    = "REMOVED"
)

var membershipTransitions = map[string][]string{
	MembershipQueued:     {MembershipDispatched, MembershipRemoved},
	MembershipDispatched: {MembershipQueued, MembershipRemoved},
	MembershipRemoved:    nil,
}

// QueueItem is one work_queue_item row: a work item's membership of exactly one
// queue.
type QueueItem struct {
	TenantID   uuid.UUID
	WorkItemID uuid.UUID
	QueueID    uuid.UUID

	Priority   int
	EligibleAt time.Time
	State      string
	Version    uint64

	EnqueuedAt time.Time
}

// Enqueue puts one work item in one queue. Because the row's primary key is the
// work item alone, enqueuing an item that is already in a queue is
// [ErrDuplicate] -- an item waits in one place, and moving it is
// [QueueStore.Move], not a second membership.
func (s QueueStore) Enqueue(ctx context.Context, ex Executor, in QueueItem) error {
	if in.EligibleAt.IsZero() || in.EnqueuedAt.IsZero() {
		return invalid("eligible_at", "a queued item carries both an enqueue and an eligibility instant")
	}
	if in.Priority == 0 {
		in.Priority = 100
	}
	if in.State == "" {
		in.State = MembershipQueued
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO work_queue_item (
			tenant_id, work_item_id, queue_id, priority, eligible_at,
			membership_state, membership_version, enqueued_at)
		VALUES ($1, $2, $3, $4, $5, $6, 1, $7)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.WorkItemID, in.QueueID, in.Priority, in.EligibleAt.UTC(),
		in.State, in.EnqueuedAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: enqueue work item %s: %w", in.WorkItemID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_queue_item %s", ErrDuplicate, in.WorkItemID)
	}
	return nil
}

// Move re-routes an item to another queue under compare-and-swap. It is an
// update of the single membership row rather than an insert, which is what
// keeps "one item, one queue" true through a re-route.
func (s QueueStore) Move(ctx context.Context, ex Executor, tenantID, workItemID, toQueue uuid.UUID,
	expectedVersion uint64, at time.Time,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if at.IsZero() {
		return invalid("eligible_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE work_queue_item
		SET queue_id = $4, eligible_at = $5, membership_state = $6,
			membership_version = membership_version + 1
		WHERE tenant_id = $1 AND work_item_id = $2 AND membership_version = $3
		  AND membership_state <> $7`,
		tenantID, workItemID, int64(expectedVersion), toQueue, at.UTC(),
		MembershipQueued, MembershipRemoved)
	if err != nil {
		return fmt.Errorf("runtimestate: move work item %s: %w", workItemID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_queue_item %s expected version %d",
			ErrVersionConflict, workItemID, expectedVersion)
	}
	return nil
}

// SetMembership advances an item's membership state under compare-and-swap.
func (s QueueStore) SetMembership(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID,
	expectedVersion uint64, next string, at time.Time,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	current, err := s.LoadItem(ctx, ex, tenantID, workItemID)
	if err != nil {
		return err
	}
	if !allows(membershipTransitions, current.State, next) {
		return fmt.Errorf("%w: work_queue_item %s: %s -> %s",
			ErrIllegalTransition, workItemID, current.State, next)
	}
	var removed any
	if next == MembershipRemoved {
		if at.IsZero() {
			return invalid("removed_at", "removing an item from a queue records when")
		}
		removed = at.UTC()
	}
	affected, err := ex.Exec(ctx, `
		UPDATE work_queue_item
		SET membership_state = $4, membership_version = membership_version + 1, removed_at = $5
		WHERE tenant_id = $1 AND work_item_id = $2 AND membership_version = $3`,
		tenantID, workItemID, int64(expectedVersion), next, removed)
	if err != nil {
		return fmt.Errorf("runtimestate: set membership of %s: %w", workItemID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_queue_item %s expected version %d",
			ErrVersionConflict, workItemID, expectedVersion)
	}
	return nil
}

// LoadItem returns one item's queue membership.
func (s QueueStore) LoadItem(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID) (QueueItem, error) {
	var (
		out     QueueItem
		version int64
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, work_item_id, queue_id, priority, eligible_at,
			membership_state, membership_version, enqueued_at
		FROM work_queue_item
		WHERE tenant_id = $1 AND work_item_id = $2`, tenantID, workItemID).Scan(
		&out.TenantID, &out.WorkItemID, &out.QueueID, &out.Priority, &out.EligibleAt,
		&out.State, &version, &out.EnqueuedAt)
	if err != nil {
		if isNoRows(err) {
			return QueueItem{}, fmt.Errorf("%w: work_queue_item %s", ErrNotFound, workItemID)
		}
		return QueueItem{}, fmt.Errorf("runtimestate: load queue membership of %s: %w", workItemID, err)
	}
	out.Version = uint64(version)
	out.EligibleAt = out.EligibleAt.UTC()
	out.EnqueuedAt = out.EnqueuedAt.UTC()
	return out, nil
}

// Claim release kinds.
const (
	ClaimCompleted = "COMPLETED"
	ClaimReleased  = "RELEASED"
	ClaimExpired   = "EXPIRED"
	ClaimRevoked   = "REVOKED"
	ClaimEscalated = "ESCALATED"
)

var claimReleaseKinds = map[string]bool{
	ClaimCompleted: true, ClaimReleased: true, ClaimExpired: true,
	ClaimRevoked: true, ClaimEscalated: true,
}

// Claim is one work_item_claim row: one hold on a work item, with how it ended.
type Claim struct {
	TenantID   uuid.UUID
	ClaimID    uuid.UUID
	WorkItemID uuid.UUID

	ClaimedBy string
	ClaimedAt time.Time
	ExpiresAt time.Time

	ReleaseKind string
	ReleasedAt  time.Time
}

// ClaimStore writes work_item_claim, the claim history behind migration 00017's
// live claim columns.
type ClaimStore struct{}

// Open records a new hold. The partial unique index admits at most one open hold
// per item, so a second claimant is [ErrDuplicate] -- claim exclusivity written
// as history rather than trusted to a caller's read-then-write.
func (s ClaimStore) Open(ctx context.Context, ex Executor, in Claim) error {
	if in.ClaimedBy == "" {
		return invalid("claimed_by", "a claim names its holder")
	}
	if in.ClaimedAt.IsZero() || !in.ExpiresAt.After(in.ClaimedAt) {
		return invalid("expires_at", "a claim expires strictly after it is taken")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO work_item_claim (
			tenant_id, claim_id, work_item_id, claimed_by, claimed_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.ClaimID, in.WorkItemID, in.ClaimedBy,
		in.ClaimedAt.UTC(), in.ExpiresAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: open claim on %s: %w", in.WorkItemID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_item_claim on %s", ErrDuplicate, in.WorkItemID)
	}
	return nil
}

// Close ends a hold, recording how it ended.
//
// The UPDATE's own WHERE clause requires the hold to still be open, so closing a
// closed claim is [ErrIllegalTransition] and writes nothing -- and migration
// 00026's work_item_claim_forbid_rewrite trigger refuses the same rewrite even to
// a caller that reaches past this store. Closing is what frees the item for the
// next claimant: the partial unique index only counts open holds.
func (s ClaimStore) Close(ctx context.Context, ex Executor, tenantID, claimID uuid.UUID,
	kind string, at time.Time,
) error {
	if !claimReleaseKinds[kind] {
		return invalid("release_kind", "kind is not a declared claim release kind")
	}
	if at.IsZero() {
		return invalid("released_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		UPDATE work_item_claim
		SET release_kind = $3, released_at = $4
		WHERE tenant_id = $1 AND claim_id = $2 AND released_at IS NULL`,
		tenantID, claimID, kind, at.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: close claim %s: %w", claimID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_item_claim %s is not an open hold", ErrIllegalTransition, claimID)
	}
	return nil
}

// OpenClaim returns the live hold on a work item, if there is one.
func (s ClaimStore) OpenClaim(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID) (Claim, error) {
	var out Claim
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, claim_id, work_item_id, claimed_by, claimed_at, expires_at
		FROM work_item_claim
		WHERE tenant_id = $1 AND work_item_id = $2 AND released_at IS NULL`,
		tenantID, workItemID).Scan(
		&out.TenantID, &out.ClaimID, &out.WorkItemID, &out.ClaimedBy, &out.ClaimedAt, &out.ExpiresAt)
	if err != nil {
		if isNoRows(err) {
			return Claim{}, fmt.Errorf("%w: no open claim on work item %s", ErrNotFound, workItemID)
		}
		return Claim{}, fmt.Errorf("runtimestate: read claim on %s: %w", workItemID, err)
	}
	out.ClaimedAt = out.ClaimedAt.UTC()
	out.ExpiresAt = out.ExpiresAt.UTC()
	return out, nil
}

// SLA breach states.
const (
	SLAWithinTarget = "WITHIN_TARGET"
	SLAAtRisk       = "AT_RISK"
	SLABreached     = "BREACHED"
	SLAEscalated    = "ESCALATED"
	SLASatisfied    = "SATISFIED"
)

var slaTransitions = map[string][]string{
	SLAWithinTarget: {SLAAtRisk, SLABreached, SLASatisfied},
	SLAAtRisk:       {SLABreached, SLASatisfied},
	SLABreached:     {SLAEscalated, SLASatisfied},
	SLAEscalated:    {SLASatisfied},
	SLASatisfied:    nil,
}

// SLA is one work_item_sla row.
type SLA struct {
	TenantID   uuid.UUID
	WorkItemID uuid.UUID

	PolicyRef   string
	TargetAt    time.Time
	EscalateAt  time.Time
	BreachState string
	BreachedAt  time.Time
	EscalatedTo string
	Version     uint64
}

// SLAStore writes and advances work_item_sla.
type SLAStore struct{}

// Set records the deadlines a work item is measured against.
func (s SLAStore) Set(ctx context.Context, ex Executor, in SLA) error {
	if in.PolicyRef == "" {
		return invalid("sla_policy_ref", "an SLA names the policy it comes from")
	}
	if in.TargetAt.IsZero() || in.EscalateAt.IsZero() {
		return invalid("target_at", "an SLA carries both a target and an escalation instant")
	}
	if in.EscalateAt.Before(in.TargetAt) {
		return invalid("escalate_at", "escalation cannot precede the target it escalates past")
	}
	if in.BreachState == "" {
		in.BreachState = SLAWithinTarget
	}
	if _, known := slaTransitions[in.BreachState]; !known {
		return invalid("breach_state", "state is not a declared SLA breach state")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO work_item_sla (
			tenant_id, work_item_id, sla_policy_ref, target_at, escalate_at,
			breach_state, sla_version)
		VALUES ($1, $2, $3, $4, $5, $6, 1)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.WorkItemID, in.PolicyRef,
		in.TargetAt.UTC(), in.EscalateAt.UTC(), in.BreachState)
	if err != nil {
		return fmt.Errorf("runtimestate: set SLA on %s: %w", in.WorkItemID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_item_sla %s", ErrDuplicate, in.WorkItemID)
	}
	return nil
}

// Advance moves an SLA to its next breach state under compare-and-swap. The
// caller supplies the observation that a deadline passed; nothing here watches a
// clock, because a sweeper is the scheduler the WF-RUN-000 gate blocks.
func (s SLAStore) Advance(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID,
	expectedVersion uint64, next string, at time.Time, escalatedTo string,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	current, err := s.Load(ctx, ex, tenantID, workItemID)
	if err != nil {
		return err
	}
	if !allows(slaTransitions, current.BreachState, next) {
		return fmt.Errorf("%w: work_item_sla %s: %s -> %s",
			ErrIllegalTransition, workItemID, current.BreachState, next)
	}
	var breachedAt any
	if next == SLABreached || next == SLAEscalated {
		if at.IsZero() {
			return invalid("breached_at", "a breached SLA records when it breached")
		}
		breachedAt = at.UTC()
	} else if !current.BreachedAt.IsZero() {
		breachedAt = current.BreachedAt.UTC()
	}
	if next == SLAEscalated && escalatedTo == "" {
		return invalid("escalated_to", "an escalated SLA names who it escalated to")
	}
	if next != SLAEscalated {
		// Every other state carries whoever it was already escalated to
		// forward, so satisfaction keeps the record of how the item finally
		// got worked instead of erasing it.
		escalatedTo = current.EscalatedTo
	}
	affected, err := ex.Exec(ctx, `
		UPDATE work_item_sla
		SET breach_state = $4, breached_at = $5, escalated_to = NULLIF($6, ''),
			sla_version = sla_version + 1
		WHERE tenant_id = $1 AND work_item_id = $2 AND sla_version = $3`,
		tenantID, workItemID, int64(expectedVersion), next, breachedAt, escalatedTo)
	if err != nil {
		return fmt.Errorf("runtimestate: advance SLA on %s: %w", workItemID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: work_item_sla %s expected version %d",
			ErrVersionConflict, workItemID, expectedVersion)
	}
	return nil
}

// Load returns one work item's SLA.
func (s SLAStore) Load(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID) (SLA, error) {
	var (
		out        SLA
		version    int64
		breachedAt *time.Time
		escalated  *string
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, work_item_id, sla_policy_ref, target_at, escalate_at,
			breach_state, breached_at, escalated_to, sla_version
		FROM work_item_sla
		WHERE tenant_id = $1 AND work_item_id = $2`, tenantID, workItemID).Scan(
		&out.TenantID, &out.WorkItemID, &out.PolicyRef, &out.TargetAt, &out.EscalateAt,
		&out.BreachState, &breachedAt, &escalated, &version)
	if err != nil {
		if isNoRows(err) {
			return SLA{}, fmt.Errorf("%w: work_item_sla %s", ErrNotFound, workItemID)
		}
		return SLA{}, fmt.Errorf("runtimestate: load SLA of %s: %w", workItemID, err)
	}
	out.Version = uint64(version)
	out.TargetAt = out.TargetAt.UTC()
	out.EscalateAt = out.EscalateAt.UTC()
	if breachedAt != nil {
		out.BreachedAt = breachedAt.UTC()
	}
	if escalated != nil {
		out.EscalatedTo = *escalated
	}
	return out, nil
}

// Approval requirement states.
const (
	RequirementDeclared  = "DECLARED"
	RequirementRouted    = "ROUTED"
	RequirementSatisfied = "SATISFIED"
	RequirementWaived    = "WAIVED"
	RequirementCancelled = "CANCELLED"
)

var requirementTransitions = map[string][]string{
	RequirementDeclared:  {RequirementRouted, RequirementWaived, RequirementCancelled},
	RequirementRouted:    {RequirementSatisfied, RequirementCancelled},
	RequirementSatisfied: nil,
	RequirementWaived:    nil,
	RequirementCancelled: nil,
}

// ApprovalRequirement is one workflow_approval_requirement row: the slot a node
// declares before any work item exists.
type ApprovalRequirement struct {
	TenantID      uuid.UUID
	RequirementID uuid.UUID
	InstanceID    uuid.UUID
	NodeID        string
	SlotKey       string

	RequirementRef       string
	SeparationConstraint string
	MaterialityClass     string
	State                string
	WorkItemID           uuid.UUID
	Version              uint64

	CreatedAt   time.Time
	SatisfiedAt time.Time
}

// ApprovalRequirementStore writes and advances workflow_approval_requirement.
type ApprovalRequirementStore struct{}

// Declare records one approval slot. Declaring the same (instance, node, slot)
// twice is [ErrDuplicate], which is the RED clause's "duplicate approval slot":
// one slot produces one work item, however many times an advancement replays.
func (s ApprovalRequirementStore) Declare(ctx context.Context, ex Executor, in ApprovalRequirement) error {
	if in.NodeID == "" || in.SlotKey == "" {
		return invalid("slot_key", "an approval slot is named by its node and slot key")
	}
	if in.RequirementRef == "" || in.SeparationConstraint == "" {
		return invalid("requirement_ref", "a slot names its requirement and separation constraint")
	}
	switch in.MaterialityClass {
	case "MATERIAL", "NON_MATERIAL":
	default:
		return invalid("materiality_class", "value is not MATERIAL or NON_MATERIAL")
	}
	if in.CreatedAt.IsZero() {
		return invalid("created_at", "timestamp is unset")
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO workflow_approval_requirement (
			tenant_id, requirement_id, instance_id, node_id, slot_key,
			requirement_ref, separation_constraint, materiality_class,
			requirement_state, requirement_version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.RequirementID, in.InstanceID, in.NodeID, in.SlotKey,
		in.RequirementRef, in.SeparationConstraint, in.MaterialityClass,
		RequirementDeclared, in.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("runtimestate: declare approval slot %s: %w", in.SlotKey, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_approval_requirement %s/%s/%s",
			ErrDuplicate, in.InstanceID, in.NodeID, in.SlotKey)
	}
	return nil
}

// Route binds a declared slot to the work item that will carry it, under
// compare-and-swap.
func (s ApprovalRequirementStore) Route(ctx context.Context, ex Executor, tenantID, requirementID, workItemID uuid.UUID,
	expectedVersion uint64,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	if workItemID == uuid.Nil {
		return invalid("work_item_id", "routing a slot names the item that carries it")
	}
	current, err := s.Load(ctx, ex, tenantID, requirementID)
	if err != nil {
		return err
	}
	if !allows(requirementTransitions, current.State, RequirementRouted) {
		return fmt.Errorf("%w: workflow_approval_requirement %s: %s -> ROUTED",
			ErrIllegalTransition, requirementID, current.State)
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_approval_requirement
		SET requirement_state = $4, work_item_id = $5, requirement_version = requirement_version + 1
		WHERE tenant_id = $1 AND requirement_id = $2 AND requirement_version = $3`,
		tenantID, requirementID, int64(expectedVersion), RequirementRouted, workItemID)
	if err != nil {
		return fmt.Errorf("runtimestate: route approval slot %s: %w", requirementID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_approval_requirement %s expected version %d",
			ErrVersionConflict, requirementID, expectedVersion)
	}
	return nil
}

// Settle moves a slot to a terminal state under compare-and-swap.
func (s ApprovalRequirementStore) Settle(ctx context.Context, ex Executor, tenantID, requirementID uuid.UUID,
	expectedVersion uint64, next string, at time.Time,
) error {
	if expectedVersion == 0 {
		return invalid("expected_version", "a compare-and-swap needs the version it expects")
	}
	current, err := s.Load(ctx, ex, tenantID, requirementID)
	if err != nil {
		return err
	}
	if !allows(requirementTransitions, current.State, next) {
		return fmt.Errorf("%w: workflow_approval_requirement %s: %s -> %s",
			ErrIllegalTransition, requirementID, current.State, next)
	}
	var satisfied any
	if next == RequirementSatisfied {
		if at.IsZero() {
			return invalid("satisfied_at", "a satisfied slot records when")
		}
		satisfied = at.UTC()
	}
	affected, err := ex.Exec(ctx, `
		UPDATE workflow_approval_requirement
		SET requirement_state = $4, satisfied_at = $5, requirement_version = requirement_version + 1
		WHERE tenant_id = $1 AND requirement_id = $2 AND requirement_version = $3`,
		tenantID, requirementID, int64(expectedVersion), next, satisfied)
	if err != nil {
		return fmt.Errorf("runtimestate: settle approval slot %s: %w", requirementID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: workflow_approval_requirement %s expected version %d",
			ErrVersionConflict, requirementID, expectedVersion)
	}
	return nil
}

// Load returns one approval requirement.
func (s ApprovalRequirementStore) Load(ctx context.Context, ex Executor, tenantID, requirementID uuid.UUID) (ApprovalRequirement, error) {
	var (
		out       ApprovalRequirement
		version   int64
		item      *uuid.UUID
		satisfied *time.Time
	)
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, requirement_id, instance_id, node_id, slot_key,
			requirement_ref, separation_constraint, materiality_class,
			requirement_state, work_item_id, requirement_version, created_at, satisfied_at
		FROM workflow_approval_requirement
		WHERE tenant_id = $1 AND requirement_id = $2`, tenantID, requirementID).Scan(
		&out.TenantID, &out.RequirementID, &out.InstanceID, &out.NodeID, &out.SlotKey,
		&out.RequirementRef, &out.SeparationConstraint, &out.MaterialityClass,
		&out.State, &item, &version, &out.CreatedAt, &satisfied)
	if err != nil {
		if isNoRows(err) {
			return ApprovalRequirement{}, fmt.Errorf("%w: workflow_approval_requirement %s", ErrNotFound, requirementID)
		}
		return ApprovalRequirement{}, fmt.Errorf("runtimestate: load approval slot %s: %w", requirementID, err)
	}
	out.Version = uint64(version)
	if item != nil {
		out.WorkItemID = *item
	}
	out.CreatedAt = out.CreatedAt.UTC()
	if satisfied != nil {
		out.SatisfiedAt = satisfied.UTC()
	}
	return out, nil
}
