package workitem

import "sort"

// Kind is the specialization a work item carries. It is the whole of the
// ApprovalTask specialization: [WorkItem.ApprovalRequirementRef] is populated
// exactly when Kind is [KindApproval], and migration 00017's
// work_item_approval_requires_requirement constraint says the same thing at
// the database layer.
type Kind string

// Declared kinds.
const (
	// KindUnspecified is the zero value and is never legal.
	KindUnspecified Kind = ""
	// KindApproval is a work item that decides one humanwork.ApprovalRequirement.
	KindApproval Kind = "APPROVAL"
	// KindTask is a work item with no approval decision behind it.
	KindTask Kind = "TASK"
)

var kindWire = map[Kind]bool{KindApproval: true, KindTask: true}

// Valid reports whether k is a declared kind.
func (k Kind) Valid() bool { return kindWire[k] }

// OwnerKind is the three-way sense in which a work item always has an owner,
// even before anyone has been resolved.
type OwnerKind string

// Declared owner kinds.
const (
	// OwnerKindUnspecified is the zero value and is never legal.
	OwnerKindUnspecified OwnerKind = ""
	// OwnerPolicyRoute means nobody has been resolved yet; the route named in
	// PolicyRouteRef owns the item until [Store.Route] runs.
	OwnerPolicyRoute OwnerKind = "POLICY_ROUTE"
	// OwnerCandidateSet means resolution produced more than one candidate; the
	// item is AVAILABLE and open for any of them to claim.
	OwnerCandidateSet OwnerKind = "CANDIDATE_SET"
	// OwnerPrincipal means resolution produced exactly one candidate, who is
	// named directly.
	OwnerPrincipal OwnerKind = "PRINCIPAL"
)

var ownerKindWire = map[OwnerKind]bool{
	OwnerPolicyRoute:  true,
	OwnerCandidateSet: true,
	OwnerPrincipal:    true,
}

// Valid reports whether k is a declared owner kind.
func (k OwnerKind) Valid() bool { return ownerKindWire[k] }

// Visibility is who may see a work item exists.
type Visibility string

// Declared visibilities.
const (
	// VisibilityUnspecified is the zero value and is never legal.
	VisibilityUnspecified       Visibility = ""
	VisibilityAssigneeOnly      Visibility = "ASSIGNEE_ONLY"
	VisibilityCandidateSet      Visibility = "CANDIDATE_SET"
	VisibilityOrganizationScope Visibility = "ORGANIZATION_SCOPE"
	VisibilityTenantGovernance  Visibility = "TENANT_GOVERNANCE"
)

var visibilityWire = map[Visibility]bool{
	VisibilityAssigneeOnly:      true,
	VisibilityCandidateSet:      true,
	VisibilityOrganizationScope: true,
	VisibilityTenantGovernance:  true,
}

// Valid reports whether v is a declared visibility.
func (v Visibility) Valid() bool { return visibilityWire[v] }

// Status is the exact lifecycle state of a work item. There is no generic
// status: WORK-001's RED clause names one as a defect, because a status that
// does not say what is happening cannot be routed on, escalated on or
// explained.
type Status string

// The eleven declared statuses, spelled exactly as migration 00017's CHECK
// constraints spell them.
const (
	// StatusUnspecified is the zero value and is never legal.
	StatusUnspecified Status = ""
	StatusCreated     Status = "CREATED"
	StatusRouted      Status = "ROUTED"
	StatusAssigned    Status = "ASSIGNED"
	StatusAvailable   Status = "AVAILABLE"
	StatusClaimed     Status = "CLAIMED"
	StatusInProgress  Status = "IN_PROGRESS"
	StatusCompleted   Status = "COMPLETED"
	StatusReturned    Status = "RETURNED"
	StatusEscalated   Status = "ESCALATED"
	StatusExpired     Status = "EXPIRED"
	StatusCancelled   Status = "CANCELLED"
)

// transitions is doc.go's lifecycle diagram, read literally:
//
//	CREATED -> ROUTED -> ASSIGNED  -> CLAIMED -> IN_PROGRESS -> COMPLETED
//	                  -> AVAILABLE                           -> RETURNED
//	                  -> ESCALATED
//	   (from any non-terminal state: ESCALATED, EXPIRED, CANCELLED)
//
// CLAIMED and IN_PROGRESS may fall back to ASSIGNED or AVAILABLE: that is the
// claim-expiry return a touch performs, not a demotion. RETURNED and
// ESCALATED may go back to ROUTED, which is what keeps a returned or
// escalated item from being stranded -- it is re-resolved by policy instead.
//
// There are deliberately no self-loops: unlike
// internal/workflow/runtime.LegalInstanceTransition, re-recording the same
// status is not treated as an idempotent no-op here, because every write this
// package makes already changes something (a claim field, an owner, a
// completion) that a same-status rewrite could otherwise be used to smuggle
// past the check.
var transitions = map[Status][]Status{
	StatusCreated:    {StatusRouted, StatusEscalated, StatusExpired, StatusCancelled},
	StatusRouted:     {StatusAssigned, StatusAvailable, StatusEscalated, StatusExpired, StatusCancelled},
	StatusAssigned:   {StatusClaimed, StatusEscalated, StatusExpired, StatusCancelled},
	StatusAvailable:  {StatusClaimed, StatusEscalated, StatusExpired, StatusCancelled},
	StatusClaimed:    {StatusInProgress, StatusAssigned, StatusAvailable, StatusEscalated, StatusExpired, StatusCancelled},
	StatusInProgress: {StatusCompleted, StatusReturned, StatusAssigned, StatusAvailable, StatusEscalated, StatusExpired, StatusCancelled},
	StatusReturned:   {StatusRouted, StatusEscalated, StatusExpired, StatusCancelled},
	StatusEscalated:  {StatusRouted, StatusExpired, StatusCancelled},
	// Terminal: no outgoing edge. History is immutable past these three.
	StatusCompleted: nil,
	StatusExpired:   nil,
	StatusCancelled: nil,
}

// Valid reports whether s is one of the eleven declared statuses.
func (s Status) Valid() bool {
	_, ok := transitions[s]
	return ok
}

// Terminal reports whether s ends the work item's lifecycle.
func (s Status) Terminal() bool {
	return s.Valid() && len(transitions[s]) == 0
}

// Claimed reports whether s is one of the two statuses that carry a live
// claim, matching migration 00017's work_item_claim_matches_status CHECK.
func (s Status) Claimed() bool {
	return s == StatusClaimed || s == StatusInProgress
}

// LegalTransition reports whether from -> to is one of the edges above.
func LegalTransition(from, to Status) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	for _, next := range transitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// Statuses returns every declared status, sorted, so a migration CHECK
// constraint and a test can be compared against one list.
func Statuses() []Status {
	out := make([]Status, 0, len(transitions))
	for s := range transitions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
