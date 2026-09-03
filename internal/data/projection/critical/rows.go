package critical

import (
	"time"

	"github.com/google/uuid"
)

// Target names which read-model table an applied event mutated.
type Target string

const (
	// TargetIntentInstance reports that an event mapped to intent_instance.
	TargetIntentInstance Target = "INTENT_INSTANCE"
	// TargetProposalRevision reports that an event mapped to
	// proposal_revision.
	TargetProposalRevision Target = "PROPOSAL_REVISION"
	// TargetNone reports that Apply advanced the checkpoint (the event was
	// genuinely new) but the schema named an event this package does not
	// project - nothing outside the checkpoint changed.
	TargetNone Target = "NONE"
)

// IntentInstanceRow is exactly the row shape
// migrations/00004_intent_and_proposal.sql's intent_instance table holds,
// decoded from one hcmnext.intents.v1.IntentInstance event.
type IntentInstanceRow struct {
	Tenant                 uuid.UUID
	IntentID               uuid.UUID
	DefinitionRef          string
	DefinitionVersion      int64
	RequestDigest          string
	RequestDigestAlgorithm string
	IdempotencyKey         string
	RequestState           string
	ExecutionState         string
	BusinessState          string
	ConsistencyState       string
	ObligationState        string
	InstanceVersion        int64
	CreatedAt              time.Time
	RecordedAt             time.Time
	LastTransitionAt       time.Time
}

// ProposalRevisionRow is exactly the row shape
// migrations/00004_intent_and_proposal.sql's proposal_revision table holds,
// decoded from one hcmnext.intents.v1.ProposalRevision event.
//
// Payload and ArtifactRef mirror the table's
// proposal_revision_payload_xor_artifact CHECK: exactly one is populated.
// This package always decodes an inline TypedPayload, so ArtifactRef is
// always empty and Payload is always populated; a future event kind that
// carries a governed artifact reference instead would populate the other.
type ProposalRevisionRow struct {
	Tenant          uuid.UUID
	IntentID        uuid.UUID
	Revision        int64
	ProposalDigest  string
	MaterialDigest  string
	DigestAlgorithm string
	SchemaRef       string
	Payload         []byte
	ArtifactRef     string
	ProducedBy      string
	ProducedAt      time.Time
}
