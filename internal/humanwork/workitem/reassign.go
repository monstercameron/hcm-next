// Package workitem: this file is WORK-004. [Store.Reassign] re-routes a work
// item whose previously routed owner or candidate set is no longer
// authorized or available -- a departure, a suspension, a leave, or a
// withdrawn authority -- rather than letting the item strand with an owner
// who can no longer act, or silently handing it to someone new with no
// evidence explaining why.
package workitem

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/humanwork"
)

// ReassignInput is [Store.Reassign]'s request: the routed work item to
// re-route, the compiled requirement it decides, and the resolution context
// to re-resolve it under -- the same shapes [ResolveAssignment] and
// [Store.Route] already take, so reassignment learns no second way to drive
// resolution.
type ReassignInput struct {
	TenantID        uuid.UUID
	WorkItemID      uuid.UUID
	ExpectedVersion int64

	// Requirement is the same compiled [humanwork.ApprovalRequirement] the
	// item was originally routed against -- re-loaded or re-compiled by the
	// caller, never re-derived here. For an APPROVAL item its RequirementID
	// must equal the item's own [WorkItem.ApprovalRequirementRef].
	Requirement humanwork.ApprovalRequirement
	// Resolution is re-run against Directory as it stands now, which is the
	// whole point: a principal who held every authority yesterday may not
	// hold it, or may not be available, today.
	Resolution humanwork.ResolutionInput
	Directory  humanwork.Directory
	Clock      humanwork.Clock

	// Meta is the caller's evidence for why this reassignment is happening
	// (a typed reason code -- "principal_departed", "authority_revoked",
	// "leave" -- never free prose) and is recorded on both transitions this
	// call produces: the move into ESCALATED and the move to the freshly
	// resolved target.
	Meta TransitionMeta
}

// Reassign is WORK-004's whole contract. When the principal or candidate set
// a work item was routed to is no longer authorized or available, Reassign
// re-resolves the item's own requirement against the directory as it now
// stands and re-routes the item to whichever authorized candidate(s)
// survive, or escalates it when none do. It never strands the item and it
// never silently rewrites who held it:
//
//   - The item first moves to ESCALATED, exactly through [Store.Escalate],
//     which invalidates any live claim outright regardless of whether that
//     claim had already expired -- WORK-004's "invalidates stale claim
//     authority".
//   - [ResolveAssignment] then re-resolves the requirement with
//     [TriggerReassignment], a distinct, auditable trigger from the item's
//     original [TriggerInitialRouting].
//   - The item is routed again, exactly through [Store.Route], to whatever
//     the fresh resolution produced: ASSIGNED to a newly authorized sole
//     candidate, AVAILABLE to a surviving candidate set, or ESCALATED again
//     when nobody remains.
//
// Both the ESCALATED transition and the final-target transition carry
// in.Meta, so the reason a relationship change triggered this call is
// immutable evidence attached to the change itself -- never a rewrite of the
// transitions the item already carried. A relationship change can only ever
// add to the evidence chain, matching WORK-004's REFACTOR clause.
//
// Reassign only accepts an item currently ASSIGNED, AVAILABLE, CLAIMED or
// IN_PROGRESS: those are the four statuses that carry a live routed owner.
// An item that has never been routed (CREATED, ROUTED) or is already back at
// its policy route (RETURNED, ESCALATED) is re-routed by [Store.Route]
// directly, and a COMPLETED, EXPIRED or CANCELLED item is terminal: its
// history, including who held it, is not something a later relationship
// change may rewrite.
func (s Store) Reassign(ctx context.Context, ex Executor, in ReassignInput) (WorkItem, error) {
	if err := in.Meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	if in.Requirement.RequirementID == "" || in.Requirement.ExpressionDigest == "" {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"reassignment requires a requirement produced by humanwork.Compile")
	}
	if in.Directory == nil {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "no directory supplied")
	}
	if in.Clock == nil {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "no clock supplied")
	}

	current, err := s.Load(ctx, ex, in.TenantID, in.WorkItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != in.ExpectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, in.TenantID, in.WorkItemID, in.ExpectedVersion)
	}
	if current.ApprovalRequirementRef != "" && current.ApprovalRequirementRef != in.Requirement.RequirementID {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"work item decides requirement %q, not %q", current.ApprovalRequirementRef, in.Requirement.RequirementID)
	}
	switch current.Status {
	case StatusAssigned, StatusAvailable, StatusClaimed, StatusInProgress:
	default:
		return WorkItem{}, refuse(CodeIllegalTransition, in.WorkItemID.String(),
			"work item at %s carries no live routed owner to reassign", current.Status)
	}

	escalated, err := s.Escalate(ctx, ex, in.TenantID, in.WorkItemID, current.ItemVersion, in.Meta)
	if err != nil {
		return WorkItem{}, err
	}

	assignment, err := ResolveAssignment(in.Requirement, in.Resolution, in.Directory, in.Clock, TriggerReassignment)
	if err != nil {
		return WorkItem{}, err
	}

	return s.Route(ctx, ex, in.TenantID, in.WorkItemID, escalated.ItemVersion, assignment, in.Meta)
}
