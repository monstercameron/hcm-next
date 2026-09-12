// Package workitem: this file is PROMOUX-003's presentation seam.
//
// [ResolveApprovalDisposition] is the one server-side projection GREEN's
// presentation clause describes: "the UI states Waiting for <role>, the
// assigned person or protected-group label, the due date, the viewer's
// acting authority, and why the action is or is not available." It never
// renders text -- that stays a locale-catalog concern in whatever surface
// consumes it (mirroring how [PermittedActions] and [MembershipOf] already
// hand back stable references and codes, not copy -- see this package's
// existing view.go) -- it only ever hands back the stable facts a renderer
// needs, so presentation consumes a disposition the domain already decided
// rather than deciding any of this itself (PROMOUX-003's REFACTOR clause).
package workitem

import (
	"time"

	"github.com/google/uuid"
)

// Disposition reason codes. Every one is a stable, redaction-safe token a
// locale catalog keys copy from; none of them is free prose.
const (
	// DispositionAuthorized reports that the viewer may currently decide
	// this item.
	DispositionAuthorized = "AUTHORIZED"
	// DispositionNoAuthority reports that the viewer holds no membership --
	// not the assignee, not a claimant, not a surviving candidate -- against
	// this item.
	DispositionNoAuthority = "NO_AUTHORITY"
	// DispositionNotActionable reports that the item's own lifecycle state
	// (terminal, or not yet claimed/started by this viewer) does not admit
	// the decide action right now, independent of authority.
	DispositionNotActionable = "NOT_ACTIONABLE"
	// DispositionSeparationConflict reports PROMOUX-003's own refusal: the
	// viewer already completed a different approval requirement on the same
	// proposal, so acting here would let one principal satisfy two distinct
	// authority classes. It shares its value with [CodeSeparationOfDuties]
	// deliberately: the same rule that refuses the write is what the
	// presentation layer explains, and a caller keying off either constant
	// keys off the identical string.
	DispositionSeparationConflict = CodeSeparationOfDuties
)

// ApprovalDisposition is the resolved presentation state of one approval
// WorkItem for one viewer at one instant: everything GREEN requires the UI
// to state, and nothing it does not need to render it.
type ApprovalDisposition struct {
	// WaitingForRef names the authority class this item is waiting on -- the
	// item's own ApprovalRequirementRef, the stable reference a "Waiting for
	// <role>" locale key resolves from.
	WaitingForRef string
	// AssignedRef is the assigned principal's identity when the item is
	// routed to exactly one owner (OwnerPrincipal), or the candidate-set
	// reference [RouteFromAssignment] minted when it is not (OwnerCandidateSet)
	// -- never both, and never empty for a routed item.
	AssignedRef string
	// AssignedIsGroup reports whether AssignedRef names a protected
	// candidate set rather than one assigned person.
	AssignedIsGroup bool
	// DueAt is the item's own deadline.
	DueAt time.Time
	// ViewerMembership is the viewer's standing against this item --
	// candidate, assignee or claimant -- which is the domain fact "the
	// viewer's acting authority" is rendered from.
	ViewerMembership Membership
	// ViewerAuthorityRef names the same authority class as WaitingForRef
	// exactly when the viewer currently holds it (ViewerMembership is not
	// MembershipNone); empty otherwise, since a viewer with no standing
	// holds no authority to name.
	ViewerAuthorityRef string
	// Available reports whether the viewer may currently decide this item.
	Available bool
	// Reason is always populated, whether or not Available is true: an
	// available action still states the authority it relies on
	// (DispositionAuthorized), and an unavailable one states the concrete,
	// stable rule that blocks it.
	Reason string
}

// ResolveApprovalDisposition computes one viewer's disposition toward item.
// siblings is every other APPROVAL work item sharing item's proposal --
// exactly [Store.LockApprovalSiblings]'s return, read without its lock
// outside a completing transaction -- so a stale or already-decided
// cross-requirement conflict is disclosed before the viewer ever attempts
// the action that would be refused.
func ResolveApprovalDisposition(item WorkItem, siblings []WorkItem, viewerPrincipalID string, now time.Time) ApprovalDisposition {
	d := ApprovalDisposition{
		WaitingForRef:   item.ApprovalRequirementRef,
		AssignedRef:     item.OwnerRef,
		AssignedIsGroup: item.OwnerKind == OwnerCandidateSet,
		DueAt:           item.DeadlineAt,
	}
	membership := MembershipOf(item, viewerPrincipalID, now)
	d.ViewerMembership = membership
	if membership != MembershipNone {
		d.ViewerAuthorityRef = item.ApprovalRequirementRef
	}

	switch {
	case membership == MembershipNone:
		d.Available, d.Reason = false, DispositionNoAuthority
	case item.Status.Terminal():
		d.Available, d.Reason = false, DispositionNotActionable
	default:
		if _, found := ConflictingCompletion(siblings, item.WorkItemID, item.ApprovalRequirementRef, viewerPrincipalID); found {
			d.Available, d.Reason = false, DispositionSeparationConflict
			break
		}
		if !actionableNow(item, membership) {
			d.Available, d.Reason = false, DispositionNotActionable
			break
		}
		d.Available, d.Reason = true, DispositionAuthorized
	}
	return d
}

// actionableNow reports whether the decide-approval action is in
// [PermittedActions]' set for this membership -- the item's own lifecycle
// gate, independent of separation of duties.
func actionableNow(item WorkItem, m Membership) bool {
	for _, a := range PermittedActions(item, m) {
		if a == ActionDecideApproval || a == ActionClaim {
			return true
		}
	}
	return false
}

// ConflictingSiblingRequirement returns the requirement reference of the
// sibling [ConflictingCompletion] found, or "" when none did. It exists so a
// caller that already has siblings loaded can explain
// [DispositionSeparationConflict] by name ("already decided <ref> for this
// proposal") without recomputing the search.
func ConflictingSiblingRequirement(siblings []WorkItem, self uuid.UUID, requirementRef, viewerPrincipalID string) string {
	if conflict, found := ConflictingCompletion(siblings, self, requirementRef, viewerPrincipalID); found {
		return conflict.ApprovalRequirementRef
	}
	return ""
}
