package workitem

import (
	"time"

	"github.com/google/uuid"
)

// Well-known reason codes this package itself mints. A caller may supply any
// non-blank semantic key as a reason -- migration 00017 does not close the
// vocabulary -- but these three are stamped by the store rather than by a
// caller, so they are exported for a projection to match on.
const (
	// ReasonCreated is the creation row's reason.
	ReasonCreated = "workitem.created"
	// ReasonClaimExpired is stamped on the transition a touch produces when it
	// discovers an expired claim and returns the item to its policy route.
	// Nothing sweeps for this; it only ever appears because a caller touched
	// the item after the expiry it itself supplied had passed.
	ReasonClaimExpired = "workitem.claim.expired"
	// ActorSystemClaimExpiry is the actor recorded on a [ReasonClaimExpired]
	// transition: the release is a property of the row and the caller's own
	// Now, not an action any principal took.
	ActorSystemClaimExpiry = "system:claim_expiry"
)

// TransitionRecord is one row of work_item_transition: immutable evidence of
// one edge the item's status took. FromStatus is empty only for the creation
// row, which states no predecessor.
type TransitionRecord struct {
	TenantID     uuid.UUID
	TransitionID uuid.UUID
	WorkItemID   uuid.UUID
	ItemVersion  int64

	FromStatus Status
	ToStatus   Status

	ActorPrincipalID string
	Reason           string
	Detail           string
	EvidenceRef      string

	At         time.Time
	RecordedAt time.Time
}

// TransitionMeta is the evidence a caller supplies for one write: who did it,
// why (a typed reason code, never free prose), an optional human-readable
// detail and evidence reference, and the business instant it happened at.
// Every [Store] write that is not an internally-generated claim release takes
// one of these.
type TransitionMeta struct {
	ActorPrincipalID string
	Reason           string
	Detail           string
	EvidenceRef      string
	At               time.Time
}

// Validate rejects a meta that could not produce a storable transition row.
func (m TransitionMeta) Validate() error {
	switch {
	case !semanticKey(m.ActorPrincipalID):
		return refuse(CodeInvalidRecord, "", "a transition must name an actor principal")
	case !semanticKey(m.Reason):
		return refuse(CodeInvalidRecord, "", "a transition must carry a typed reason code, not free prose")
	case m.At.IsZero():
		return refuse(CodeInvalidRecord, "", "at must be supplied; this package never reads a wall clock")
	}
	return nil
}
