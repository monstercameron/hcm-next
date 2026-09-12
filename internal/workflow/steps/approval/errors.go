package approval

import "errors"

var (
	// ErrInvalidContinuation reports malformed or contradictory durable
	// continuation data.
	ErrInvalidContinuation = errors.New("workflow approval: invalid continuation")
	// ErrBindingMismatch reports evidence bound to a different node,
	// requirement, proposal, work item, or principal.
	ErrBindingMismatch = errors.New("workflow approval: binding mismatch")
	// ErrInvalidEvent reports an event outside the closed resolution vocabulary.
	ErrInvalidEvent = errors.New("workflow approval: invalid event")
	// ErrInvalidEvidence reports a completed WorkItem without the exact immutable
	// ApprovalDecision whose digest it records.
	ErrInvalidEvidence = errors.New("workflow approval: invalid evidence")
	// ErrSeparationConflict reports PROMOUX-003's core refusal: the decision's
	// approver already completed a different approval requirement on the same
	// proposal. It is returned by [Complete], never by [Resolve], because the
	// conflict is about who may act, not about how a completed row resolves.
	ErrSeparationConflict = errors.New("workflow approval: separation of duties refuses this completion")
)
