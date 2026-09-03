package approval

import (
	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// Requirement is the immutable subset of an ApprovalRequirement the runtime
// needs while a node is parked. WorkItemIDs are the exact human-work records
// allowed to satisfy it.
type Requirement struct {
	RequirementID       string
	RequirementRevision uint64
	RequirementDigest   string
	Quorum              uint32
	Distinct            bool
	DecideBy            values.Instant
	Expiry              values.Instant
	WorkItemIDs         []uuid.UUID
}

// Continuation is the durable, proposal-bound condition an APPROVAL node is
// waiting to satisfy. Digest is minted by NewContinuation over every field.
type Continuation struct {
	WorkflowInstanceID uuid.UUID
	NodeID             string
	ProposalRevisionID string
	ProposalDigest     digest.Reference
	Requirements       []Requirement
	Digest             string
}

// EventKind names an explicit caller-supplied reason to resolve a continuation.
// DecisionsChanged is the normal event after a WorkItem completion.
type EventKind string

const (
	EventDecisionsChanged EventKind = "DECISIONS_CHANGED"
	EventInvalidated      EventKind = "INVALIDATED"
	EventCancelled        EventKind = "CANCELLED"
)

// Event is the external fact Resolve is evaluating. Prior makes replay
// semantic: a duplicate event returns the original resolution verbatim.
type Event struct {
	Kind   EventKind
	Reason string
	Prior  *Resolution
}

// Resolution is the immutable outcome of evaluating a Continuation. An empty
// Outcome means the node still awaits human work.
type Resolution struct {
	ContinuationDigest string
	Outcome            workflow.Outcome
	DecisionRefs       []string
	WorkItemRefs       []string
	ResolvedAt         values.Instant
	Reason             string
	Digest             string
}
