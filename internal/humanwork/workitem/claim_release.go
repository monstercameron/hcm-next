// Package workitem: this file is EP-WORK-002's domain half. The transport
// endpoints ClaimWorkItem and ReleaseWorkItem call [Store.ClaimCurrent] and
// [Store.Release] rather than [Store.Claim] directly, because a wire request
// carries only a caller-asserted work item id, expected version and
// principal -- nothing that can be trusted as "this caller currently holds
// authority" on its own. Both methods re-derive that authority fresh, from
// the item exactly as it stands right now, and only then hand off to the
// item_version compare-and-swap and append-only evidence write [Store.Claim]
// and [Store.simpleTransition] already provide. Nothing about exclusivity,
// CAS or evidence is reimplemented here.
package workitem

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeUnauthorizedClaimant reports that the caller named in a claim or
// release request does not currently stand as the item's assignee, an
// unexcluded candidate, or -- for a release -- its live claimant. It is
// evaluated against the item as freshly loaded by this call, never against a
// membership fact the caller computed from an earlier read: a delegation
// that has since expired, or a candidate since excluded, refuses here even
// though it might have been authorized when the caller last looked.
const CodeUnauthorizedClaimant = "UNAUTHORIZED_CLAIMANT"

// ClaimCurrentInput is [Store.ClaimCurrent]'s request: the endpoint
// boundary's claim request narrowed to exactly what a current-authority
// claim needs.
type ClaimCurrentInput struct {
	TenantID        uuid.UUID
	WorkItemID      uuid.UUID
	ExpectedVersion int64

	ClaimantPrincipalID string
	ClaimExpiresAt      time.Time
	Now                 time.Time

	Meta TransitionMeta
}

// ClaimCurrent authorizes and performs one claim in a single call: it loads
// the item fresh, refuses a caller-asserted version that does not match the
// stored one, and refuses a claimant whose current standing -- computed by
// [MembershipOf] against the item's own recorded assignment at Now, not
// against anything the caller supplied -- is neither the assignee nor an
// authorized candidate. Only a caller that passes both checks reaches
// [Store.Claim], which performs the actual item_version compare-and-swap and
// the exclusive, append-only evidence write; a caller refused here writes
// nothing.
func (s Store) ClaimCurrent(ctx context.Context, ex Executor, in ClaimCurrentInput) (WorkItem, error) {
	if !semanticKey(in.ClaimantPrincipalID) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "a claim must name a claimant")
	}
	if in.Now.IsZero() {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "a claim must carry Now")
	}
	current, err := s.Load(ctx, ex, in.TenantID, in.WorkItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != in.ExpectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, in.TenantID, in.WorkItemID, in.ExpectedVersion)
	}
	switch MembershipOf(current, in.ClaimantPrincipalID, in.Now) {
	case MembershipAssignee, MembershipCandidate:
	default:
		return WorkItem{}, refuse(CodeUnauthorizedClaimant, in.WorkItemID.String(),
			"%s is not the item's current assignee or an authorized candidate", in.ClaimantPrincipalID)
	}
	// ClaimCurrentInput and ClaimInput share the same fields in the same
	// order by construction: ClaimCurrent adds only the authorization check
	// above, so the conversion carries every field across unchanged.
	return s.Claim(ctx, ex, ClaimInput(in))
}

// ReleaseInput is [Store.Release]'s request: the current claimant giving up
// a live claim without completing it.
type ReleaseInput struct {
	TenantID        uuid.UUID
	WorkItemID      uuid.UUID
	ExpectedVersion int64

	ReleasingPrincipalID string
	Now                  time.Time

	Meta TransitionMeta
}

// Release is EP-WORK-002's voluntary release: the current claimant gives up
// a live CLAIMED or IN_PROGRESS claim, returning the item to its routed
// owner -- ASSIGNED when a principal was named directly, AVAILABLE when the
// item is owned by a candidate set -- exactly [releaseExpiredClaim]'s own
// target computation, reused rather than re-derived.
//
// Authority is the live claim itself, re-checked against the row as loaded
// right now: only the principal the stored row currently names as
// claimed_by may release. If the claim being released has already expired
// against Now, the touch this call performs releases it to its policy route
// as an expiry -- not as this caller's own release -- and the call is
// refused [CodeClaimExpired], exactly as [Store.Start], [Store.Complete] and
// [Store.Return] already behave. An expired lease is therefore never
// released as if it were still current: the caller cannot complete "their"
// release against a claim that is, as of this call, no longer theirs to
// release.
func (s Store) Release(ctx context.Context, ex Executor, in ReleaseInput) (WorkItem, error) {
	if err := in.Meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	if !semanticKey(in.ReleasingPrincipalID) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "a release must name the releasing principal")
	}
	if in.Now.IsZero() {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "a release must carry Now")
	}
	current, released, err := s.touchClaim(ctx, ex, in.TenantID, in.WorkItemID, in.ExpectedVersion, in.Now)
	if err != nil {
		return WorkItem{}, err
	}
	if released {
		return current, refuse(CodeClaimExpired, in.WorkItemID.String(),
			"claim expired; item returned to its policy route")
	}
	if !current.Status.Claimed() {
		return WorkItem{}, refuse(CodeIllegalTransition, in.WorkItemID.String(),
			"a work item at %s carries no live claim to release", current.Status)
	}
	if current.ClaimedBy != in.ReleasingPrincipalID {
		return WorkItem{}, refuse(CodeUnauthorizedClaimant, in.WorkItemID.String(),
			"work item is claimed by a different principal than the caller")
	}
	target := StatusAvailable
	if current.OwnerKind == OwnerPrincipal {
		target = StatusAssigned
	}
	return s.simpleTransition(ctx, ex, current, target, in.Meta)
}
