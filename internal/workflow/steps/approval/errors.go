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
)
