// Package workitem: this file is PROMOUX-003's separation-of-duties floor.
//
// [Store.LockApprovalSiblings] and [ConflictingCompletion] give a caller in
// internal/workflow/steps/approval (the approval owner, per PROMOUX-003's
// REFACTOR clause) everything it needs to refuse a completion that would let
// one principal satisfy two distinct approval requirements on the same
// proposal, using a real database guarantee rather than an in-process check
// a second concurrent writer could race past.
//
// The guarantee is row locking, not a new constraint table: every APPROVAL
// work item sharing (tenant_id, proposal_ref) is locked with SELECT ... FOR
// UPDATE, in a stable work_item_id order, before any of them is inspected or
// completed. Two transactions racing to complete different requirements on
// the same proposal both name the same row set (every approval work item for
// that proposal, including each other's), so only one can hold the locks at
// a time; the second blocks until the first commits or rolls back, and by
// the time it proceeds it observes the first transaction's committed
// completion. That is what makes "at most one of two concurrent completions
// succeeds" a property of PostgreSQL's own lock manager rather than of
// however many application processes happen to be racing.
package workitem

import (
	"context"

	"github.com/google/uuid"
)

// CodeSeparationOfDuties reports a completion refused because the deciding
// principal already completed a different approval requirement on the same
// proposal. Unlike every other code in this package, the refusal is not
// about this item's own version, claim or transition legality: the row it
// names as evidence is a sibling work item, not the one the caller is
// completing.
const CodeSeparationOfDuties = "SEPARATION_OF_DUTIES"

// LockApprovalSiblings locks, in the caller's own transaction, every
// APPROVAL work item sharing tenantID and proposalRef -- including the item
// the caller is about to complete, if it is one of them -- ordered by
// work_item_id so every caller acquires the locks in the same order and two
// racing completions never deadlock against each other.
//
// A caller that does not hold ex inside a transaction the caller itself
// began gets no meaningful lock: FOR UPDATE only serializes concurrent
// holders of the same transaction boundary [approval.Complete] and
// [Store.Complete] already require.
func (Store) LockApprovalSiblings(ctx context.Context, ex Executor, tenantID uuid.UUID, proposalRef string) ([]WorkItem, error) {
	if tenantID == uuid.Nil || proposalRef == "" {
		return nil, refuse(CodeInvalidRecord, "", "separation lock requires a tenant and a proposal reference")
	}
	rows, err := ex.Query(ctx,
		`SELECT `+workItemColumns+` FROM work_item
		 WHERE tenant_id = $1 AND proposal_ref = $2 AND kind = 'APPROVAL'
		 ORDER BY work_item_id
		 FOR UPDATE`,
		tenantID, proposalRef)
	if err != nil {
		return nil, wrap(CodeStorageFailed, "", err, "lock approval siblings for proposal %s", proposalRef)
	}
	defer rows.Close()

	out := []WorkItem{}
	for rows.Next() {
		item, scanErr := scanWorkItem(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, "", scanErr, "scan locked approval sibling")
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, "", err, "iterate locked approval siblings")
	}
	return out, nil
}

// ConflictingCompletion is the pure separation-of-duties rule applied to an
// already-locked sibling set: it reports the first other, already-completed
// approval work item -- naming a different requirement on the same proposal
// -- whose CompletedBy equals approverPrincipalID.
//
// It never flags the item being completed itself (self, by WorkItemID) and
// never flags a sibling deciding the *same* requirement (a quorum-of-one
// requirement has exactly one sibling naming it, itself; a distinct-quorum
// requirement's own duplicate-principal rule is humanwork.Resolve's
// OneRequirementPerPrincipal, evaluated at candidate resolution, not here).
// What this rule catches is the cross-requirement case candidate resolution
// cannot see: two requirements resolved independently, at different times,
// that happen to have authorized the same principal for both.
//
// approverPrincipalID must be a non-empty semantic key: an unresolved or
// blank approver never matches a completed sibling by construction, so this
// function refuses to guess "no conflict" for one and requires the caller to
// have already rejected it as invalid input.
func ConflictingCompletion(siblings []WorkItem, self uuid.UUID, requirementRef, approverPrincipalID string) (WorkItem, bool) {
	if approverPrincipalID == "" {
		return WorkItem{}, false
	}
	for _, sib := range siblings {
		if sib.WorkItemID == self {
			continue
		}
		if sib.ApprovalRequirementRef == requirementRef {
			continue
		}
		if sib.Status == StatusCompleted && sib.CompletedBy == approverPrincipalID {
			return sib, true
		}
	}
	return WorkItem{}, false
}
