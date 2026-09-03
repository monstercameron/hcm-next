package critical

import (
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
)

// Schema references this package's default [Mapper] recognizes. They follow
// the "<message full name>@<schema version>" convention already used by
// internal/intent/app/pgstore and the fixtures in internal/data/ledger and
// internal/data/ledger/hashchain.
const (
	SchemaRefIntentInstance   = "hcmnext.intents.v1.IntentInstance@1"
	SchemaRefProposalRevision = "hcmnext.intents.v1.ProposalRevision@1"
)

// Mapper decodes one ledger event's typed payload into the read-model row it
// projects to. It is a port (rather than this package hard-wiring protobuf
// decoding into [Apply] and [Verify]) so a caller can register additional
// schema versions, or substitute a fake in tests, without this package
// changing.
//
// A method returns ok=false, nil error when schemaRef names an event kind
// the method does not own; [Apply] and [Verify] try MapIntentInstance and
// MapProposalRevision in that order and treat "neither claimed it" as
// [ErrUnsupportedSchema]. A non-nil error means the schema was recognized
// but the payload failed to decode or validate - that is always reported,
// never swallowed as "not mine."
type Mapper interface {
	MapIntentInstance(schemaRef string, payload []byte) (row IntentInstanceRow, ok bool, err error)
	MapProposalRevision(schemaRef string, payload []byte) (row ProposalRevisionRow, ok bool, err error)
}

// ProtoMapper decodes the two generated kernel messages
// hcmnext.intents.v1.IntentInstance and hcmnext.intents.v1.ProposalRevision
// (gen/go/hcmnext/intents/v1/business_intent.pb.go) under [SchemaRefIntentInstance]
// and [SchemaRefProposalRevision]. It holds no state and is safe to share.
type ProtoMapper struct{}

var _ Mapper = ProtoMapper{}

// MapIntentInstance implements [Mapper].
func (ProtoMapper) MapIntentInstance(schemaRef string, payload []byte) (IntentInstanceRow, bool, error) {
	if schemaRef != SchemaRefIntentInstance {
		return IntentInstanceRow{}, false, nil
	}

	var msg intentsv1.IntentInstance
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: decode %s: %w", schemaRef, err)
	}

	intentID, err := uuid.Parse(msg.GetIntentId())
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: intent_id %q: %w", schemaRef, msg.GetIntentId(), err)
	}
	definition := msg.GetDefinition()
	if definition == nil || definition.GetIntentTypeId() == "" {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: definition reference is required", schemaRef)
	}
	digestRef := msg.GetCanonicalRequestDigest()
	if digestRef == nil || digestRef.GetDigest() == "" || digestRef.GetAlgorithmId() == "" {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: canonical request digest is required", schemaRef)
	}
	if msg.GetIdempotencyKey() == "" {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: idempotency key is required", schemaRef)
	}
	lifecycle := msg.GetLifecycle()
	if lifecycle == nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: lifecycle dimensions are required", schemaRef)
	}
	requestState, err := requestStateText(lifecycle.GetRequest())
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: %w", schemaRef, err)
	}
	executionState, err := executionStateText(lifecycle.GetExecution())
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: %w", schemaRef, err)
	}
	businessState, err := businessStateText(lifecycle.GetBusiness())
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: %w", schemaRef, err)
	}
	consistencyState, err := consistencyStateText(lifecycle.GetConsistency())
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: %w", schemaRef, err)
	}
	obligationState, err := obligationStateText(lifecycle.GetObligation())
	if err != nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: %w", schemaRef, err)
	}
	if msg.GetInstanceVersion() == 0 {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: instance_version must be at least 1", schemaRef)
	}
	createdAt := msg.GetCreatedAt()
	recordedAt := msg.GetRecordedAt()
	lastTransitionAt := msg.GetLastTransitionAt()
	if createdAt == nil || recordedAt == nil || lastTransitionAt == nil {
		return IntentInstanceRow{}, true, fmt.Errorf("critical: %s: created_at, recorded_at and last_transition_at are required", schemaRef)
	}

	return IntentInstanceRow{
		IntentID:               intentID,
		DefinitionRef:          definition.GetIntentTypeId(),
		DefinitionVersion:      int64(definition.GetVersion()),
		RequestDigest:          digestRef.GetDigest(),
		RequestDigestAlgorithm: digestRef.GetAlgorithmId(),
		IdempotencyKey:         msg.GetIdempotencyKey(),
		RequestState:           requestState,
		ExecutionState:         executionState,
		BusinessState:          businessState,
		ConsistencyState:       consistencyState,
		ObligationState:        obligationState,
		InstanceVersion:        int64(msg.GetInstanceVersion()),
		CreatedAt:              createdAt.AsTime(),
		RecordedAt:             recordedAt.AsTime(),
		LastTransitionAt:       lastTransitionAt.AsTime(),
	}, true, nil
}

// MapProposalRevision implements [Mapper].
func (ProtoMapper) MapProposalRevision(schemaRef string, payload []byte) (ProposalRevisionRow, bool, error) {
	if schemaRef != SchemaRefProposalRevision {
		return ProposalRevisionRow{}, false, nil
	}

	var msg intentsv1.ProposalRevision
	if err := proto.Unmarshal(payload, &msg); err != nil {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: decode %s: %w", schemaRef, err)
	}

	intentID, err := uuid.Parse(msg.GetIntentId())
	if err != nil {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: intent_id %q: %w", schemaRef, msg.GetIntentId(), err)
	}
	if msg.GetRevision() == 0 {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: revision must be at least 1", schemaRef)
	}
	material := msg.GetMaterialProposalDigest()
	if material == nil || material.GetDigest() == "" || material.GetAlgorithmId() == "" {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: material proposal digest is required", schemaRef)
	}
	proposal := msg.GetProposal()
	if proposal == nil || len(proposal.GetProtobufWireBytes()) == 0 {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: inline proposal payload is required", schemaRef)
	}
	schema := proposal.GetSchema()
	if schema == nil || schema.GetSchemaId() == "" {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: proposal payload schema is required", schemaRef)
	}
	proposalDigestRef := proposal.GetCanonicalDigest()
	if proposalDigestRef == nil || proposalDigestRef.GetDigest() == "" {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: proposal payload canonical digest is required", schemaRef)
	}
	createdBy := msg.GetCreatedBy()
	if createdBy == nil || createdBy.GetPrincipalId() == "" {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: created_by principal is required", schemaRef)
	}
	createdAt := msg.GetCreatedAt()
	if createdAt == nil {
		return ProposalRevisionRow{}, true, fmt.Errorf("critical: %s: created_at is required", schemaRef)
	}

	return ProposalRevisionRow{
		IntentID:        intentID,
		Revision:        int64(msg.GetRevision()),
		ProposalDigest:  proposalDigestRef.GetDigest(),
		MaterialDigest:  material.GetDigest(),
		DigestAlgorithm: material.GetAlgorithmId(),
		SchemaRef:       fmt.Sprintf("%s@%d", schema.GetSchemaId(), schema.GetVersion()),
		Payload:         proposal.GetProtobufWireBytes(),
		ProducedBy:      createdBy.GetPrincipalId(),
		ProducedAt:      createdAt.AsTime(),
	}, true, nil
}

func requestStateText(s intentsv1.RequestState) (string, error) {
	switch s {
	case intentsv1.RequestState_REQUEST_STATE_DRAFT:
		return "DRAFT", nil
	case intentsv1.RequestState_REQUEST_STATE_PREFLIGHTED:
		return "PREFLIGHTED", nil
	case intentsv1.RequestState_REQUEST_STATE_SIMULATED:
		return "SIMULATED", nil
	case intentsv1.RequestState_REQUEST_STATE_SUBMITTED:
		return "SUBMITTED", nil
	case intentsv1.RequestState_REQUEST_STATE_APPROVED:
		return "APPROVED", nil
	case intentsv1.RequestState_REQUEST_STATE_REJECTED:
		return "REJECTED", nil
	case intentsv1.RequestState_REQUEST_STATE_WITHDRAWN:
		return "WITHDRAWN", nil
	case intentsv1.RequestState_REQUEST_STATE_CANCELLED:
		return "CANCELLED", nil
	case intentsv1.RequestState_REQUEST_STATE_SUPERSEDED:
		return "SUPERSEDED", nil
	case intentsv1.RequestState_REQUEST_STATE_CLOSED:
		return "CLOSED", nil
	case intentsv1.RequestState_REQUEST_STATE_REOPENED:
		return "REOPENED", nil
	default:
		return "", fmt.Errorf("request state %v has no intent_instance.request_state mapping", s)
	}
}

func executionStateText(s intentsv1.ExecutionState) (string, error) {
	switch s {
	case intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED:
		return "NOT_PLANNED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED:
		return "SCHEDULED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_REVALIDATING:
		return "REVALIDATING", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING:
		return "EXECUTING", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED:
		return "COMMITTED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED:
		return "BLOCKED", nil
	case intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED:
		return "REPAIR_REQUIRED", nil
	default:
		return "", fmt.Errorf("execution state %v has no intent_instance.execution_state mapping", s)
	}
}

func businessStateText(s intentsv1.BusinessState) (string, error) {
	switch s {
	case intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED:
		return "NOT_STARTED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS:
		return "IN_PROGRESS", nil
	case intentsv1.BusinessState_BUSINESS_STATE_COMPLETED:
		return "COMPLETED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_NOT_ACHIEVED:
		return "NOT_ACHIEVED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_CORRECTED:
		return "CORRECTED", nil
	case intentsv1.BusinessState_BUSINESS_STATE_UNKNOWN:
		return "UNKNOWN", nil
	default:
		return "", fmt.Errorf("business state %v has no intent_instance.business_state mapping", s)
	}
}

func consistencyStateText(s intentsv1.ConsistencyState) (string, error) {
	switch s {
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE:
		return "NOT_APPLICABLE", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION:
		return "PENDING_OBSERVATION", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT:
		return "CONSISTENT", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED:
		return "DEGRADED", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING:
		return "REPAIRING", nil
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_UNKNOWN:
		return "UNKNOWN", nil
	default:
		return "", fmt.Errorf("consistency state %v has no intent_instance.consistency_state mapping", s)
	}
}

func obligationStateText(s intentsv1.ObligationState) (string, error) {
	switch s {
	case intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE:
		return "NOT_APPLICABLE", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_PENDING:
		return "PENDING", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED:
		return "SATISFIED", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE:
		return "OVERDUE", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_WAIVED:
		return "WAIVED", nil
	case intentsv1.ObligationState_OBLIGATION_STATE_UNKNOWN:
		return "UNKNOWN", nil
	default:
		return "", fmt.Errorf("obligation state %v has no intent_instance.obligation_state mapping", s)
	}
}
