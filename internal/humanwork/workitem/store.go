package workitem

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it.
//
// Every write below issues at least two statements -- the item-row
// compare-and-set and the append-only transition insert -- and this package
// never opens a transaction of its own: pass a [dbport.Tx] the caller began
// and will finish. On a bare connection those statements would commit
// independently, and a crash between them would leave a state change nothing
// explains.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Port is the surface a durable driver calls: Create, Route, Claim, Start,
// Complete, Return, Escalate, Expire, Cancel, Reassign, Load,
// ListForInstance. There is no lease-expiry sweep and no timer here --
// WF-RUN-000 gates every one of those behind the P1B re-evaluation, and every
// instant a method below needs is supplied by the caller.
type Port interface {
	Create(ctx context.Context, ex Executor, item WorkItem, meta TransitionMeta) (WorkItem, error)
	Route(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, assignment Assignment, meta TransitionMeta) (WorkItem, error)
	Claim(ctx context.Context, ex Executor, in ClaimInput) (WorkItem, error)
	Start(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, now time.Time, meta TransitionMeta) (WorkItem, error)
	Complete(ctx context.Context, ex Executor, in CompleteInput) (WorkItem, error)
	Return(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, now time.Time, meta TransitionMeta) (WorkItem, error)
	Escalate(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, meta TransitionMeta) (WorkItem, error)
	Expire(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, meta TransitionMeta) (WorkItem, error)
	Cancel(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, meta TransitionMeta) (WorkItem, error)
	// Reassign is WORK-004: re-resolve and re-route a work item whose
	// previously routed owner or candidate set is no longer authorized or
	// available.
	Reassign(ctx context.Context, ex Executor, in ReassignInput) (WorkItem, error)
	Load(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID) (WorkItem, error)
	ListForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]WorkItem, error)
}

// Store implements [Port] over migrations/00017_work_item.sql. It holds no
// state; every method takes its [Executor] explicitly.
type Store struct{}

var _ Port = Store{}

const workItemColumns = `tenant_id, work_item_id, item_version, kind, work_type, status,
	correlation_id, workflow_instance_id, node_id, approval_requirement_ref, proposal_ref,
	subject_refs, owner_kind, owner_ref, policy_route_ref, visibility, organization_scope_id,
	deadline_at, assignment, assignment_digest, claim_id, claimed_by, claimed_at, claim_expires_at,
	completed_by, completed_at, completed_output_digest, created_at, recorded_at`

const transitionColumns = `tenant_id, transition_id, work_item_id, item_version,
	from_status, to_status, actor_principal_id, reason, detail, evidence_ref, at, recorded_at`

// ClaimInput is [Store.Claim]'s request: the version the caller holds, who is
// claiming, the caller-supplied claim expiry, and the caller-supplied Now
// against which an already-expired claim on the row is discovered and
// released before the new claim is attempted.
type ClaimInput struct {
	TenantID        uuid.UUID
	WorkItemID      uuid.UUID
	ExpectedVersion int64

	ClaimantPrincipalID string
	ClaimExpiresAt      time.Time
	Now                 time.Time

	Meta TransitionMeta
}

// CompleteInput is [Store.Complete]'s request.
type CompleteInput struct {
	TenantID        uuid.UUID
	WorkItemID      uuid.UUID
	ExpectedVersion int64

	CompletedBy           string
	CompletedOutputDigest string
	Now                   time.Time

	Meta TransitionMeta
}

// Create stores a new work item at version 1 and appends its CREATED
// transition row in the same statement pair. An item whose identity already
// exists collides on the primary key rather than overwriting.
func (s Store) Create(ctx context.Context, ex Executor, item WorkItem, meta TransitionMeta) (WorkItem, error) {
	if item.Status != StatusCreated {
		return WorkItem{}, refuse(CodeInvalidRecord, item.WorkItemID.String(),
			"a new work item must be created at status CREATED, got %s", item.Status)
	}
	if item.ItemVersion != 1 {
		return WorkItem{}, refuse(CodeInvalidRecord, item.WorkItemID.String(),
			"a new work item must be created at version 1, got %d", item.ItemVersion)
	}
	if err := item.Validate(); err != nil {
		return WorkItem{}, err
	}
	if err := meta.Validate(); err != nil {
		return WorkItem{}, err
	}

	assignJSON, err := marshalAssignment(item.Assignment)
	if err != nil {
		return WorkItem{}, wrap(CodeInvalidRecord, item.WorkItemID.String(), err, "encode assignment")
	}

	row := ex.QueryRow(ctx, `
		INSERT INTO work_item (`+workItemColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, COALESCE($29, now()))
		RETURNING `+workItemColumns,
		item.TenantID, item.WorkItemID, item.ItemVersion, string(item.Kind), item.WorkType, string(item.Status),
		item.CorrelationID, item.WorkflowInstanceID, item.NodeID,
		nullableText(item.ApprovalRequirementRef), nullableText(item.ProposalRef),
		textArray(item.SubjectRefs), string(item.OwnerKind), item.OwnerRef, item.PolicyRouteRef,
		string(item.Visibility), item.OrganizationScopeID,
		item.DeadlineAt.UTC(), assignJSON, nullableText(item.AssignmentDigest),
		nullUUID(item.ClaimID), nullableText(item.ClaimedBy), utcOrNil(item.ClaimedAt), utcOrNil(item.ClaimExpiresAt),
		nullableText(item.CompletedBy), utcOrNil(item.CompletedAt), nullableText(item.CompletedOutputDigest),
		item.CreatedAt.UTC(), zeroTimeOrNil(item.RecordedAt))

	stored, err := scanWorkItem(row)
	if err != nil {
		return WorkItem{}, wrap(CodeStorageFailed, item.WorkItemID.String(), err, "insert work item")
	}
	if err := s.insertTransition(ctx, ex, TransitionRecord{
		TenantID: item.TenantID, TransitionID: uuid.New(), WorkItemID: item.WorkItemID,
		ItemVersion: stored.ItemVersion, FromStatus: "", ToStatus: StatusCreated,
		ActorPrincipalID: meta.ActorPrincipalID, Reason: meta.Reason, Detail: meta.Detail,
		EvidenceRef: meta.EvidenceRef, At: meta.At,
	}); err != nil {
		return WorkItem{}, err
	}
	return stored, nil
}

// Route applies a resolved [Assignment], moving the item to ASSIGNED,
// AVAILABLE or ESCALATED per [RouteFromAssignment]. It is legal from CREATED
// (initial routing) and from RETURNED or ESCALATED (re-resolution).
//
// The move is doc.go's two literal edges, CREATED/RETURNED/ESCALATED -> ROUTED
// -> {ASSIGNED,AVAILABLE,ESCALATED}, and both are recorded: entering routing
// is one transition row, and the resolved outcome is the next, each with its
// own item_version. That is what lets a projection see "this item entered
// resolution at 12:03 and resolved to ASSIGNED at 12:04" as two distinct,
// separately timed facts rather than one write that elides the first.
func (s Store) Route(
	ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64,
	assignment Assignment, meta TransitionMeta,
) (WorkItem, error) {
	if err := meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	current, err := s.Load(ctx, ex, tenantID, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != expectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, tenantID, workItemID, expectedVersion)
	}
	if !LegalTransition(current.Status, StatusRouted) {
		return WorkItem{}, refuse(CodeIllegalTransition, workItemID.String(),
			"work item may not enter routing from %s", current.Status)
	}
	routed, err := s.simpleTransition(ctx, ex, current, StatusRouted, TransitionMeta{
		ActorPrincipalID: meta.ActorPrincipalID, Reason: ReasonRoutingStarted, At: meta.At,
	})
	if err != nil {
		return WorkItem{}, err
	}

	ownerKind, ownerRef, target := RouteFromAssignment(assignment, routed.PolicyRouteRef)
	if !LegalTransition(routed.Status, target) {
		return WorkItem{}, refuse(CodeIllegalTransition, workItemID.String(),
			"work item may not route from %s to %s", routed.Status, target)
	}

	assignJSON, err := marshalAssignment(assignment)
	if err != nil {
		return WorkItem{}, wrap(CodeInvalidRecord, workItemID.String(), err, "encode assignment")
	}
	digest, err := assignment.Digest()
	if err != nil {
		return WorkItem{}, wrap(CodeInvalidRecord, workItemID.String(), err, "digest assignment")
	}

	row := ex.QueryRow(ctx, `
		UPDATE work_item SET
			status = $1, owner_kind = $2, owner_ref = $3, assignment = $4, assignment_digest = $5,
			item_version = item_version + 1
		WHERE tenant_id = $6 AND work_item_id = $7 AND item_version = $8
		RETURNING `+workItemColumns,
		string(target), string(ownerKind), ownerRef, assignJSON, digest,
		tenantID, workItemID, routed.ItemVersion)
	updated, err := scanWorkItem(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return WorkItem{}, s.explainLost(ctx, ex, tenantID, workItemID, routed.ItemVersion)
		}
		return WorkItem{}, wrap(CodeStorageFailed, workItemID.String(), err, "route work item")
	}
	if err := s.insertTransition(ctx, ex, TransitionRecord{
		TenantID: tenantID, TransitionID: uuid.New(), WorkItemID: workItemID,
		ItemVersion: updated.ItemVersion, FromStatus: routed.Status, ToStatus: target,
		ActorPrincipalID: meta.ActorPrincipalID, Reason: meta.Reason, Detail: meta.Detail,
		EvidenceRef: meta.EvidenceRef, At: meta.At,
	}); err != nil {
		return WorkItem{}, err
	}
	return updated, nil
}

// Claim is WORK-003's exclusive claim. Exactly one caller among any number
// racing on the same ExpectedVersion wins the item_version compare-and-set;
// every other one is refused [CodeAlreadyClaimed] and mutates nothing. If the
// item's current claim has already expired against in.Now, this method
// releases it first -- its own transition, in the same caller transaction --
// and then attempts the new claim against the released state.
func (s Store) Claim(ctx context.Context, ex Executor, in ClaimInput) (WorkItem, error) {
	if err := in.Meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	if !semanticKey(in.ClaimantPrincipalID) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "a claim must name a claimant")
	}
	if in.Now.IsZero() || in.ClaimExpiresAt.IsZero() {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "a claim must carry Now and an expiry")
	}
	if !in.ClaimExpiresAt.After(in.Now) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "claim expiry must be after now")
	}

	current, err := s.Load(ctx, ex, in.TenantID, in.WorkItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != in.ExpectedVersion {
		return WorkItem{}, refuse(CodeAlreadyClaimed, in.WorkItemID.String(),
			"writer holds item version %d, stored version is %d", in.ExpectedVersion, current.ItemVersion)
	}

	if current.Status.Claimed() && current.ClaimExpired(in.Now) {
		released, relErr := s.releaseExpiredClaim(ctx, ex, current, in.Now)
		if relErr != nil {
			return WorkItem{}, relErr
		}
		current = released
	}

	if current.Status != StatusAssigned && current.Status != StatusAvailable {
		if current.Status.Claimed() {
			return WorkItem{}, refuse(CodeAlreadyClaimed, in.WorkItemID.String(),
				"work item is already claimed by %s", current.ClaimedBy)
		}
		return WorkItem{}, refuse(CodeIllegalTransition, in.WorkItemID.String(),
			"a work item at %s may not be claimed", current.Status)
	}

	claimID := uuid.New()
	row := ex.QueryRow(ctx, `
		UPDATE work_item SET
			status = 'CLAIMED', claim_id = $1, claimed_by = $2, claimed_at = $3, claim_expires_at = $4,
			item_version = item_version + 1
		WHERE tenant_id = $5 AND work_item_id = $6 AND item_version = $7
		RETURNING `+workItemColumns,
		claimID, in.ClaimantPrincipalID, in.Now.UTC(), in.ClaimExpiresAt.UTC(),
		in.TenantID, in.WorkItemID, current.ItemVersion)
	updated, err := scanWorkItem(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			// Another writer's UPDATE committed between our read above and this
			// statement: exactly the race WORK-003 exists to decide, and this
			// caller lost it.
			return WorkItem{}, refuse(CodeAlreadyClaimed, in.WorkItemID.String(),
				"lost the claim race for this work item")
		}
		return WorkItem{}, wrap(CodeStorageFailed, in.WorkItemID.String(), err, "claim work item")
	}
	if err := s.insertTransition(ctx, ex, TransitionRecord{
		TenantID: in.TenantID, TransitionID: uuid.New(), WorkItemID: in.WorkItemID,
		ItemVersion: updated.ItemVersion, FromStatus: current.Status, ToStatus: StatusClaimed,
		ActorPrincipalID: in.Meta.ActorPrincipalID, Reason: in.Meta.Reason, Detail: in.Meta.Detail,
		EvidenceRef: in.Meta.EvidenceRef, At: in.Meta.At,
	}); err != nil {
		return WorkItem{}, err
	}
	return updated, nil
}

// releaseExpiredClaim moves a CLAIMED or IN_PROGRESS item whose claim has
// expired back to its policy route -- ASSIGNED when the route had named a
// principal, AVAILABLE when it had named a candidate set -- and records the
// release as its own transition. It is never swept: it runs only inside
// [Store.Claim], [Store.Start], [Store.Complete] and [Store.Return], each of
// which is itself only ever invoked by a caller touching the item.
func (s Store) releaseExpiredClaim(ctx context.Context, ex Executor, current WorkItem, now time.Time) (WorkItem, error) {
	target := StatusAvailable
	if current.OwnerKind == OwnerPrincipal {
		target = StatusAssigned
	}
	row := ex.QueryRow(ctx, `
		UPDATE work_item SET
			status = $1, claim_id = NULL, claimed_by = NULL, claimed_at = NULL, claim_expires_at = NULL,
			item_version = item_version + 1
		WHERE tenant_id = $2 AND work_item_id = $3 AND item_version = $4
		RETURNING `+workItemColumns,
		string(target), current.TenantID, current.WorkItemID, current.ItemVersion)
	updated, err := scanWorkItem(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return WorkItem{}, refuse(CodeAlreadyClaimed, current.WorkItemID.String(),
				"another writer already released or claimed this item")
		}
		return WorkItem{}, wrap(CodeStorageFailed, current.WorkItemID.String(), err, "release expired claim")
	}
	if err := s.insertTransition(ctx, ex, TransitionRecord{
		TenantID: current.TenantID, TransitionID: uuid.New(), WorkItemID: current.WorkItemID,
		ItemVersion: updated.ItemVersion, FromStatus: current.Status, ToStatus: target,
		ActorPrincipalID: ActorSystemClaimExpiry, Reason: ReasonClaimExpired,
		Detail: "claim held by " + current.ClaimedBy + " expired",
		At:     now,
	}); err != nil {
		return WorkItem{}, err
	}
	return updated, nil
}

// touchClaim loads the current item and, when it carries a claim expired
// against now, releases it. The bool return reports whether a release
// happened; when it did, the returned item is the released one and the
// caller must not proceed with whatever operation it was about to perform.
func (s Store) touchClaim(
	ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, now time.Time,
) (WorkItem, bool, error) {
	current, err := s.Load(ctx, ex, tenantID, workItemID)
	if err != nil {
		return WorkItem{}, false, err
	}
	if current.ItemVersion != expectedVersion {
		return WorkItem{}, false, s.explainLost(ctx, ex, tenantID, workItemID, expectedVersion)
	}
	if current.Status.Claimed() && current.ClaimExpired(now) {
		released, err := s.releaseExpiredClaim(ctx, ex, current, now)
		if err != nil {
			return WorkItem{}, false, err
		}
		return released, true, nil
	}
	return current, false, nil
}

// Start moves a claimed item to IN_PROGRESS. If the claim it would act on has
// already expired against now, the claim is released instead and the call is
// refused with [CodeClaimExpired]; the item returned is the released one.
func (s Store) Start(
	ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64,
	now time.Time, meta TransitionMeta,
) (WorkItem, error) {
	if err := meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	current, released, err := s.touchClaim(ctx, ex, tenantID, workItemID, expectedVersion, now)
	if err != nil {
		return WorkItem{}, err
	}
	if released {
		return current, refuse(CodeClaimExpired, workItemID.String(),
			"claim expired; item returned to its policy route")
	}
	return s.simpleTransition(ctx, ex, current, StatusInProgress, meta)
}

// Complete records a work item's accepted output. The output digest is
// immutable once set: migration 00017's work_item_forbid_rewrite trigger
// enforces this even against this package's own store, and Complete only
// ever attempts the write from CLAIMED or IN_PROGRESS, never from COMPLETED.
func (s Store) Complete(ctx context.Context, ex Executor, in CompleteInput) (WorkItem, error) {
	if err := in.Meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	if !semanticKey(in.CompletedBy) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "completion must name who completed it")
	}
	if !ValidDigest(in.CompletedOutputDigest) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"completed output digest %q is malformed", in.CompletedOutputDigest)
	}
	current, released, err := s.touchClaim(ctx, ex, in.TenantID, in.WorkItemID, in.ExpectedVersion, in.Now)
	if err != nil {
		return WorkItem{}, err
	}
	if released {
		return current, refuse(CodeClaimExpired, in.WorkItemID.String(),
			"claim expired; item returned to its policy route")
	}
	if !LegalTransition(current.Status, StatusCompleted) {
		return WorkItem{}, refuse(CodeIllegalTransition, in.WorkItemID.String(),
			"work item may not move from %s to COMPLETED", current.Status)
	}

	row := ex.QueryRow(ctx, `
		UPDATE work_item SET
			status = 'COMPLETED', completed_by = $1, completed_at = $2, completed_output_digest = $3,
			claim_id = NULL, claimed_by = NULL, claimed_at = NULL, claim_expires_at = NULL,
			item_version = item_version + 1
		WHERE tenant_id = $4 AND work_item_id = $5 AND item_version = $6
		RETURNING `+workItemColumns,
		in.CompletedBy, in.Now.UTC(), in.CompletedOutputDigest,
		in.TenantID, in.WorkItemID, current.ItemVersion)
	updated, err := scanWorkItem(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return WorkItem{}, s.explainLost(ctx, ex, in.TenantID, in.WorkItemID, current.ItemVersion)
		}
		return WorkItem{}, wrap(CodeStorageFailed, in.WorkItemID.String(), err, "complete work item")
	}
	if err := s.insertTransition(ctx, ex, TransitionRecord{
		TenantID: in.TenantID, TransitionID: uuid.New(), WorkItemID: in.WorkItemID,
		ItemVersion: updated.ItemVersion, FromStatus: current.Status, ToStatus: StatusCompleted,
		ActorPrincipalID: in.Meta.ActorPrincipalID, Reason: in.Meta.Reason, Detail: in.Meta.Detail,
		EvidenceRef: in.Meta.EvidenceRef, At: in.Meta.At,
	}); err != nil {
		return WorkItem{}, err
	}
	return updated, nil
}

// Return moves an IN_PROGRESS item back to RETURNED, releasing its claim. If
// the claim it would act on has already expired against now, the touch
// releases it to the policy route instead and refuses with
// [CodeClaimExpired].
func (s Store) Return(
	ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64,
	now time.Time, meta TransitionMeta,
) (WorkItem, error) {
	if err := meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	current, released, err := s.touchClaim(ctx, ex, tenantID, workItemID, expectedVersion, now)
	if err != nil {
		return WorkItem{}, err
	}
	if released {
		return current, refuse(CodeClaimExpired, workItemID.String(),
			"claim expired; item returned to its policy route")
	}
	return s.simpleTransition(ctx, ex, current, StatusReturned, meta)
}

// Escalate moves an item to ESCALATED from any status that allows it,
// releasing any claim it holds outright rather than checking its expiry: an
// escalation supersedes the claim regardless of whether it had already lapsed.
func (s Store) Escalate(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, meta TransitionMeta) (WorkItem, error) {
	if err := meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	current, err := s.Load(ctx, ex, tenantID, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != expectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, tenantID, workItemID, expectedVersion)
	}
	return s.simpleTransition(ctx, ex, current, StatusEscalated, meta)
}

// Expire moves an item to EXPIRED. Nothing in this package decides when to
// call it: the deadline is a business instant the caller alone observes.
func (s Store) Expire(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, meta TransitionMeta) (WorkItem, error) {
	if err := meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	current, err := s.Load(ctx, ex, tenantID, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != expectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, tenantID, workItemID, expectedVersion)
	}
	return s.simpleTransition(ctx, ex, current, StatusExpired, meta)
}

// Cancel moves an item to CANCELLED.
func (s Store) Cancel(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expectedVersion int64, meta TransitionMeta) (WorkItem, error) {
	if err := meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	current, err := s.Load(ctx, ex, tenantID, workItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != expectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, tenantID, workItemID, expectedVersion)
	}
	return s.simpleTransition(ctx, ex, current, StatusCancelled, meta)
}

// simpleTransition is the shared body of Start, Return, Escalate, Expire and
// Cancel: a status change plus, whenever the item is leaving CLAIMED or
// IN_PROGRESS, clearing the claim it held (migration 00017's
// work_item_claim_matches_status requires the claim columns to be all-or-none
// with exactly those two statuses).
func (s Store) simpleTransition(ctx context.Context, ex Executor, current WorkItem, target Status, meta TransitionMeta) (WorkItem, error) {
	if !LegalTransition(current.Status, target) {
		return WorkItem{}, refuse(CodeIllegalTransition, current.WorkItemID.String(),
			"work item may not move from %s to %s", current.Status, target)
	}
	clearClaim := current.Status.Claimed() && !target.Claimed()
	row := ex.QueryRow(ctx, `
		UPDATE work_item SET
			status = $1,
			claim_id = CASE WHEN $2 THEN NULL ELSE claim_id END,
			claimed_by = CASE WHEN $2 THEN NULL ELSE claimed_by END,
			claimed_at = CASE WHEN $2 THEN NULL ELSE claimed_at END,
			claim_expires_at = CASE WHEN $2 THEN NULL ELSE claim_expires_at END,
			item_version = item_version + 1
		WHERE tenant_id = $3 AND work_item_id = $4 AND item_version = $5
		RETURNING `+workItemColumns,
		string(target), clearClaim, current.TenantID, current.WorkItemID, current.ItemVersion)
	updated, err := scanWorkItem(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return WorkItem{}, s.explainLost(ctx, ex, current.TenantID, current.WorkItemID, current.ItemVersion)
		}
		return WorkItem{}, wrap(CodeStorageFailed, current.WorkItemID.String(), err, "transition work item to %s", target)
	}
	if err := s.insertTransition(ctx, ex, TransitionRecord{
		TenantID: current.TenantID, TransitionID: uuid.New(), WorkItemID: current.WorkItemID,
		ItemVersion: updated.ItemVersion, FromStatus: current.Status, ToStatus: target,
		ActorPrincipalID: meta.ActorPrincipalID, Reason: meta.Reason, Detail: meta.Detail,
		EvidenceRef: meta.EvidenceRef, At: meta.At,
	}); err != nil {
		return WorkItem{}, err
	}
	return updated, nil
}

// Load reads one work item by identity. A missing row and a row belonging to
// another tenant are the same answer, [CodeWorkItemNotFound]: this method
// never discloses that an item it may not see exists. Load is a read and
// therefore not a touch: it reports the stored claim as it stands and never
// releases an expired one. Use [WorkItem.ClaimExpired] for the verdict.
func (Store) Load(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID) (WorkItem, error) {
	row := ex.QueryRow(ctx,
		`SELECT `+workItemColumns+` FROM work_item WHERE tenant_id = $1 AND work_item_id = $2`,
		tenantID, workItemID)
	item, err := scanWorkItem(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return WorkItem{}, refuse(CodeWorkItemNotFound, workItemID.String(), "no such work item")
		}
		return WorkItem{}, wrap(CodeStorageFailed, workItemID.String(), err, "read work item")
	}
	return item, nil
}

// ListForInstance reads every work item the frontier has created for one
// workflow instance, ordered by node id then creation time.
func (Store) ListForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]WorkItem, error) {
	rows, err := ex.Query(ctx,
		`SELECT `+workItemColumns+` FROM work_item
		 WHERE tenant_id = $1 AND workflow_instance_id = $2
		 ORDER BY node_id, created_at`,
		tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), err, "list work items for instance")
	}
	defer rows.Close()

	out := []WorkItem{}
	for rows.Next() {
		item, scanErr := scanWorkItem(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), scanErr, "scan work item")
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), err, "iterate work items")
	}
	return out, nil
}

// LoadTransitions reads every recorded transition for one work item, ordered
// by item_version -- the evidence chain the UNIQUE (tenant_id, work_item_id,
// item_version) constraint makes gap-free and fork-free by construction. It
// is not part of [Port]: a durable driver never needs it, but a test proving
// that a write appended evidence does.
func (Store) LoadTransitions(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID) ([]TransitionRecord, error) {
	rows, err := ex.Query(ctx,
		`SELECT `+transitionColumns+` FROM work_item_transition
		 WHERE tenant_id = $1 AND work_item_id = $2
		 ORDER BY item_version`,
		tenantID, workItemID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, workItemID.String(), err, "list work item transitions")
	}
	defer rows.Close()

	out := []TransitionRecord{}
	for rows.Next() {
		t, scanErr := scanTransition(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, workItemID.String(), scanErr, "scan work item transition")
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, workItemID.String(), err, "iterate work item transitions")
	}
	return out, nil
}

func (Store) insertTransition(ctx context.Context, ex Executor, t TransitionRecord) error {
	_, err := ex.Exec(ctx, `
		INSERT INTO work_item_transition (`+transitionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($12, now()))`,
		t.TenantID, t.TransitionID, t.WorkItemID, t.ItemVersion,
		nullableStatus(t.FromStatus), string(t.ToStatus),
		t.ActorPrincipalID, t.Reason, t.Detail, nullableText(t.EvidenceRef),
		t.At.UTC(), zeroTimeOrNil(t.RecordedAt))
	if err != nil {
		return wrap(CodeStorageFailed, t.WorkItemID.String(), err, "insert work item transition")
	}
	return nil
}

// explainLost distinguishes the two reasons a version-guarded update matched
// nothing: the item is gone (or not this tenant's), or another writer got
// there first.
func (Store) explainLost(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID, expected int64) error {
	var stored int64
	err := ex.QueryRow(ctx,
		`SELECT item_version FROM work_item WHERE tenant_id = $1 AND work_item_id = $2`,
		tenantID, workItemID).Scan(&stored)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return refuse(CodeWorkItemNotFound, workItemID.String(), "no such work item")
		}
		return wrap(CodeStorageFailed, workItemID.String(), err, "read work item version")
	}
	return refuse(CodeStaleItem, workItemID.String(),
		"writer holds item version %d, stored version is %d", expected, stored)
}

func scanWorkItem(row dbport.Row) (WorkItem, error) {
	var (
		w                WorkItem
		kind             string
		status           string
		approvalReqRef   *string
		proposalRef      *string
		ownerKind        string
		visibility       string
		assignmentRaw    []byte
		assignmentDigest *string
		claimID          *uuid.UUID
		claimedBy        *string
		claimedAt        *time.Time
		claimExpiresAt   *time.Time
		completedBy      *string
		completedAt      *time.Time
		completedDigest  *string
	)
	err := row.Scan(
		&w.TenantID, &w.WorkItemID, &w.ItemVersion, &kind, &w.WorkType, &status,
		&w.CorrelationID, &w.WorkflowInstanceID, &w.NodeID, &approvalReqRef, &proposalRef,
		&w.SubjectRefs, &ownerKind, &w.OwnerRef, &w.PolicyRouteRef, &visibility, &w.OrganizationScopeID,
		&w.DeadlineAt, &assignmentRaw, &assignmentDigest, &claimID, &claimedBy, &claimedAt, &claimExpiresAt,
		&completedBy, &completedAt, &completedDigest, &w.CreatedAt, &w.RecordedAt)
	if err != nil {
		return WorkItem{}, err
	}
	w.Kind = Kind(kind)
	w.Status = Status(status)
	w.ApprovalRequirementRef = derefText(approvalReqRef)
	w.ProposalRef = derefText(proposalRef)
	w.OwnerKind = OwnerKind(ownerKind)
	w.Visibility = Visibility(visibility)
	w.AssignmentDigest = derefText(assignmentDigest)
	w.ClaimID = claimID
	w.ClaimedBy = derefText(claimedBy)
	w.ClaimedAt = utcOrNilTime(claimedAt)
	w.ClaimExpiresAt = utcOrNilTime(claimExpiresAt)
	w.CompletedBy = derefText(completedBy)
	w.CompletedAt = utcOrNilTime(completedAt)
	w.CompletedOutputDigest = derefText(completedDigest)
	w.DeadlineAt = w.DeadlineAt.UTC()
	w.CreatedAt = w.CreatedAt.UTC()
	w.RecordedAt = w.RecordedAt.UTC()
	if len(assignmentRaw) > 0 {
		if err := json.Unmarshal(assignmentRaw, &w.Assignment); err != nil {
			return WorkItem{}, err
		}
	}
	return w, nil
}

func scanTransition(row dbport.Row) (TransitionRecord, error) {
	var (
		t           TransitionRecord
		fromStatus  *string
		toStatus    string
		evidenceRef *string
	)
	err := row.Scan(
		&t.TenantID, &t.TransitionID, &t.WorkItemID, &t.ItemVersion,
		&fromStatus, &toStatus, &t.ActorPrincipalID, &t.Reason, &t.Detail, &evidenceRef,
		&t.At, &t.RecordedAt)
	if err != nil {
		return TransitionRecord{}, err
	}
	if fromStatus != nil {
		t.FromStatus = Status(*fromStatus)
	}
	t.ToStatus = Status(toStatus)
	t.EvidenceRef = derefText(evidenceRef)
	t.At = t.At.UTC()
	t.RecordedAt = t.RecordedAt.UTC()
	return t, nil
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableStatus(s Status) any {
	if s == "" {
		return nil
	}
	return string(s)
}

func nullUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

func derefText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func textArray(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func utcOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func utcOrNilTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func zeroTimeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
