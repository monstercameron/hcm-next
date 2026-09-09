package approval

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
)

// Complete records decision.Digest() as the immutable completed output of the
// ApprovalTask work item it decided.
//
// It checks that the decision names exactly this item's requirement and
// proposal and that its approver is the authorized candidate this item's own
// WORK-002 assignment recorded -- the same binding [Resolve] later re-checks
// when it reads the completed item back, so an item this call accepts can
// never fail that later check. Quorum and multi-item route evaluation remain
// [Resolve]'s job; Complete only ever writes the one item its caller names.
//
// A double claim, an unauthorized owner and a duplicate conflicting
// completion are refused by [workitem.Store.Complete] itself: the item
// version compare-and-set, the claim-expiry touch, and migration 00017's
// immutable-output trigger (surfaced here as an illegal transition once the
// item is already COMPLETED) are what make Complete safe to call from more
// than one racing writer.
func Complete(
	ctx context.Context, tx workitem.Executor, store workitem.Port,
	item workitem.WorkItem, decision intentapproval.ApprovalDecision,
	now time.Time, meta workitem.TransitionMeta,
) (workitem.WorkItem, error) {
	if item.Kind != workitem.KindApproval || item.ApprovalRequirementRef == "" {
		return workitem.WorkItem{}, fmt.Errorf("%w: work item %s is not an approval task", ErrBindingMismatch, item.WorkItemID)
	}
	if !decision.Outcome.Valid() {
		return workitem.WorkItem{}, fmt.Errorf("%w: decision outcome %q is not declared", ErrInvalidEvidence, decision.Outcome)
	}
	b := decision.Binding
	if b.RequirementID != item.ApprovalRequirementRef {
		return workitem.WorkItem{}, fmt.Errorf("%w: decision names requirement %q, item names %q", ErrBindingMismatch, b.RequirementID, item.ApprovalRequirementRef)
	}
	if b.ProposalDigest.Digest != item.ProposalRef {
		return workitem.WorkItem{}, fmt.Errorf("%w: decision names another proposal", ErrBindingMismatch)
	}
	candidate, ok := item.Assignment.Resolution.Authorizes(decision.Approver.PrincipalID)
	if !ok {
		return workitem.WorkItem{}, fmt.Errorf("%w: %q is not the recorded candidate for work item %s", ErrBindingMismatch, decision.Approver.PrincipalID, item.WorkItemID)
	}
	if candidate.Via != decision.Approver.Via || candidate.DelegationID != decision.Approver.DelegationID {
		return workitem.WorkItem{}, fmt.Errorf("%w: approver route does not match the recorded candidate", ErrBindingMismatch)
	}
	digest := decision.Digest()
	if digest == "" {
		return workitem.WorkItem{}, fmt.Errorf("%w: decision has no digest", ErrInvalidEvidence)
	}

	completed, err := store.Complete(ctx, tx, workitem.CompleteInput{
		TenantID:              item.TenantID,
		WorkItemID:            item.WorkItemID,
		ExpectedVersion:       item.ItemVersion,
		CompletedBy:           decision.Approver.PrincipalID,
		CompletedOutputDigest: digest,
		Now:                   now,
		Meta:                  meta,
	})
	if err != nil {
		return workitem.WorkItem{}, err
	}
	// WORK-010: the full decision content is recorded in the same transaction
	// as its completion, digest-verified against completed.CompletedOutputDigest
	// (which is exactly the digest above -- store.Complete recorded it
	// verbatim). A failure here rolls back the completion too, since both run
	// through the same tx.
	if err := recordDecision(ctx, tx, completed, decision); err != nil {
		return workitem.WorkItem{}, err
	}
	return completed, nil
}
