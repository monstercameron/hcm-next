package workitem

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
)

// Membership is how one principal stands to one work item at one instant:
// the read-side answer to "may this caller see this row, and may they act on
// it". It is computed from the stored item alone -- its claim fields, its
// owner, and the assignment evidence [Store.Route] recorded -- plus the
// caller-stated instant, exactly the values a reader already holds. Nothing
// here grants organization-scope or governance sight; [Visible] takes those
// two caller-side facts separately.
//
// The ordering matters: a claimant outranks an assignee outranks a
// candidate, so callers may compare memberships with >=.
type Membership int

// Membership grades, weakest to strongest.
const (
	// MembershipNone means the principal is no party to the item: not the
	// owner, not a live claimant, not a current unexcluded candidate.
	MembershipNone Membership = iota
	// MembershipCandidate means the item is owned by a candidate set and the
	// principal is a current, unexcluded, unexpired member of it.
	MembershipCandidate
	// MembershipAssignee means the item is routed to the principal directly.
	MembershipAssignee
	// MembershipClaimant means the principal holds the item's live claim.
	MembershipClaimant
)

// MembershipOf computes the caller's membership at now. The rules are the
// store's write rules read backwards:
//
//   - A live claim (claimed_by = ref, claim_expires_at after now) is the
//     strongest standing. An expired claim confers none: the item has
//     reverted to its policy route in read semantics exactly as
//     [Store.releaseExpiredClaim] would move it on the next write.
//   - A principal owner (OwnerPrincipal with owner_ref = ref) is the
//     assignee.
//   - A candidate-set owner makes the principal a candidate when the
//     recorded [Assignment] names them in the resolution's candidates, the
//     exclusion list does not, and -- for delegated candidates -- the
//     delegation has not expired by now. That is the RED clause "a
//     stale/delegated/recused principal sees the item": a recused principal
//     is in Excluded, a stale delegate's DelegationExpiry has passed, and
//     neither is ever a member.
func MembershipOf(item WorkItem, principalRef string, now time.Time) Membership {
	if !semanticKey(principalRef) {
		return MembershipNone
	}
	if item.ClaimedBy == principalRef && !item.ClaimExpired(now) {
		return MembershipClaimant
	}
	switch item.OwnerKind {
	case OwnerPrincipal:
		if item.OwnerRef == principalRef {
			return MembershipAssignee
		}
	case OwnerCandidateSet:
		if candidateMember(item.Assignment, principalRef, now) {
			return MembershipCandidate
		}
	}
	return MembershipNone
}

// candidateMember reports whether ref survives the recorded resolution: in
// Candidates, absent from Excluded, and -- when the candidacy came by
// delegation -- inside the delegation window.
func candidateMember(a Assignment, ref string, now time.Time) bool {
	for _, e := range a.Resolution.Excluded {
		if e.PrincipalID == ref {
			return false
		}
	}
	for _, c := range a.Resolution.Candidates {
		if c.PrincipalID != ref {
			continue
		}
		if c.Via == humanwork.SourceDelegated &&
			c.DelegationExpiry.IsSet() && !c.DelegationExpiry.Time().After(now) {
			continue
		}
		return true
	}
	return false
}

// Visible reports whether the item's visibility class admits this caller.
// Membership always admits. Past that the stored class decides:
// ORGANIZATION_SCOPE admits a caller inside the item's organization scope,
// TENANT_GOVERNANCE admits a caller the endpoint authorized for governance
// visibility, and the two restricted classes admit nobody else. A caller who
// is not visible is indistinguishable from a caller whose item does not
// exist: the endpoint projects both to the same non-disclosing answer.
func Visible(item WorkItem, m Membership, inOrganizationScope, governed bool) bool {
	if m != MembershipNone {
		return true
	}
	switch item.Visibility {
	case VisibilityOrganizationScope:
		return inOrganizationScope
	case VisibilityTenantGovernance:
		return governed
	default:
		return false
	}
}

// Action is one wire-visible action token the read endpoints compute server
// side. The strings are the permitted_actions field values.
type Action string

// Action tokens, named after the WorkService method each permits.
const (
	ActionClaim          Action = "claim"
	ActionRelease        Action = "release"
	ActionComplete       Action = "complete"
	ActionDecideApproval Action = "decide_approval"
)

// PermittedActions computes the action set the caller may exercise against
// the item at now, derived from membership and the same lifecycle edges
// [Store.Claim], [Store.Start], [Store.Complete] and [Store.Return] enforce
// on write -- the endpoint advertises an action exactly when the write
// would accept it from this caller. Terminal items admit nothing.
//
//   - claim: ASSIGNED to the caller, or AVAILABLE to a candidate set they
//     belong to.
//   - release: the caller holds a live claim (CLAIMED or IN_PROGRESS).
//   - complete: the caller's claim is live and the item is IN_PROGRESS, the
//     only edge Complete accepts.
//   - decide_approval: complete is permitted and the item is an approval.
func PermittedActions(item WorkItem, m Membership) []Action {
	if item.Status.Terminal() {
		return nil
	}
	out := []Action{}
	switch m {
	case MembershipAssignee:
		if item.Status == StatusAssigned {
			out = append(out, ActionClaim)
		}
	case MembershipCandidate:
		if item.Status == StatusAvailable {
			out = append(out, ActionClaim)
		}
	case MembershipClaimant:
		if item.Status == StatusClaimed || item.Status == StatusInProgress {
			out = append(out, ActionRelease)
		}
		if item.Status == StatusInProgress {
			out = append(out, ActionComplete)
			if item.Kind == KindApproval && item.ProposalRef != "" {
				out = append(out, ActionDecideApproval)
			}
		}
	}
	return out
}

// ContextVisible reports whether the identity-bearing context fields -- the
// resolved candidate set and the assigned principal -- may be disclosed to
// this caller. Membership reveals them; a caller admitted only by
// organization scope or governance sees the item's existence, status and
// deadlines but not who else may act on it.
func ContextVisible(m Membership) bool {
	return m != MembershipNone
}

// EvidenceVisible reports whether the restricted evidence compartment --
// input artifact refs and the like -- may be disclosed to this caller. The
// compartment rule (compartment.go) is that assignment authority never
// implies artifact access: a mere candidate sees the item to claim it but
// not its evidence; the assignee or claimant who must act on it does. A
// viewer admitted by scope or governance alone never sees evidence.
func EvidenceVisible(m Membership) bool {
	return m == MembershipAssignee || m == MembershipClaimant
}
