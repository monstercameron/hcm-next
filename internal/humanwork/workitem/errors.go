package workitem

import (
	"errors"
	"fmt"
)

// Sentinels. Classify with [errors.Is]; read [Error.Code] for the exact
// reason. Never match message text.
var (
	// ErrWorkItem is the sentinel every refusal from this package unwraps to.
	ErrWorkItem = errors.New("workitem: write refused")
)

// Stable refusal codes.
const (
	// CodeInvalidRecord reports a work item, transition or assignment this
	// package refuses to store because it is incomplete or self-contradictory
	// -- WORK-001's RED clause: missing correlation, subject, owner, visibility
	// or deadline, or a status/kind/owner-kind/visibility this package does not
	// declare.
	CodeInvalidRecord = "INVALID_RECORD"
	// CodeIllegalTransition reports a status change [LegalTransition] does not
	// allow.
	CodeIllegalTransition = "ILLEGAL_TRANSITION"
	// CodeWorkItemNotFound reports a work item that does not exist for the
	// tenant, which is also what a cross-tenant read looks like: this package
	// never discloses that an item it may not see exists.
	CodeWorkItemNotFound = "WORK_ITEM_NOT_FOUND"
	// CodeStaleItem reports a writer whose view of item_version has been
	// overtaken by another write. It is the concurrency story for every
	// operation except Claim, which has its own, more specific code below.
	CodeStaleItem = "CONFLICT_STALE_ITEM"
	// CodeAlreadyClaimed is WORK-003's whole exclusivity contract: exactly one
	// concurrent claimant wins the item_version compare-and-swap, and every
	// loser -- whether it lost to a live claim or lost the race to release and
	// reclaim an expired one -- is refused this code and mutates nothing.
	CodeAlreadyClaimed = "ALREADY_CLAIMED"
	// CodeClaimExpired reports that [Store.Start], [Store.Complete] or
	// [Store.Return] found the claim it was about to act on already expired
	// against the caller's own Now. The operation is refused, and the item the
	// call returns alongside this code is the released one: ASSIGNED or
	// AVAILABLE, not the state the caller thought it was acting on.
	CodeClaimExpired = "CLAIM_EXPIRED"
	// CodeOutputImmutable reports an attempt to record a completed output
	// digest that differs from one already set. The migration's
	// work_item_forbid_rewrite trigger enforces the same rule at the database
	// layer; this code is this package's own guard before a statement is even
	// sent.
	CodeOutputImmutable = "OUTPUT_IMMUTABLE"
	// CodeStorageFailed reports a database failure underneath a well-formed
	// request.
	CodeStorageFailed = "STORAGE_FAILED"
	// CodeStaleProposal reports EP-WORK-003's own conformance clause: the
	// caller's asserted proposal revision no longer matches the approval work
	// item's current one. It is evaluated before the current-authority
	// recheck: a decision bound to a proposal that has since moved must never
	// reach authority evaluation, let alone the CAS.
	CodeStaleProposal = "STALE_PROPOSAL"
)

// Error is one typed refusal, naming the code, the work item it happened at,
// and the underlying cause.
type Error struct {
	Code       string
	WorkItemID string
	Detail     string
	Err        error
}

func (e *Error) Error() string {
	loc := ""
	if e.WorkItemID != "" {
		loc = " for work item " + e.WorkItemID
	}
	msg := fmt.Sprintf("workitem: %s%s: %s", e.Code, loc, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrWorkItem, e.Err}
	}
	return []error{ErrWorkItem}
}

// refuse builds a typed refusal.
func refuse(code, workItemID, format string, args ...any) *Error {
	return &Error{Code: code, WorkItemID: workItemID, Detail: fmt.Sprintf(format, args...)}
}

// wrap builds a typed refusal around an underlying cause.
func wrap(code, workItemID string, err error, format string, args ...any) *Error {
	return &Error{Code: code, WorkItemID: workItemID, Detail: fmt.Sprintf(format, args...), Err: err}
}

// CodeOf returns the refusal code carried by err, or "" when err is not a
// refusal from this package.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
