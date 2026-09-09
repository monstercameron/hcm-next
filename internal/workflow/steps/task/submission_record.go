package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// submissionWire is WORK-010's JSON encoding of one [Submission], minted for
// the work_item_decision row [Submit] records in the same transaction as the
// item's own completion.
//
// It is a dedicated wire struct, not a direct json.Marshal(sub), for the
// same reason internal/workflow/steps/approval's decisionWire exists:
// [Submission.ClaimExpiresAt] and [Submission.SubmittedAt] are
// [values.Instant], whose type carries no exported fields at all, so
// encoding/json's default reflection produces an empty object for them.
// Every instant here is carried as its canonical RFC 3339 text instead.
type submissionWire struct {
	WorkflowInstanceID string `json:"workflow_instance_id"`
	NodeID             string `json:"node_id"`
	WorkItemID         string `json:"work_item_id"`
	ItemVersion        int64  `json:"item_version"`

	CompletedBy  string `json:"completed_by"`
	CandidateVia string `json:"candidate_via"`
	DelegationID string `json:"delegation_id,omitempty"`

	ClaimID        string `json:"claim_id"`
	ClaimExpiresAt string `json:"claim_expires_at"`
	SubmittedAt    string `json:"submitted_at"`

	OutputSchema             schemaRefWire `json:"output_schema"`
	CanonicalPayloadDigest   string        `json:"canonical_payload_digest"`
	FormDefinition           VersionedRef  `json:"form_definition"`
	RenderContextDigest      string        `json:"render_context_digest"`
	ValidationEvidenceRef    string        `json:"validation_evidence_ref"`
	AccessibilityEvidenceRef string        `json:"accessibility_evidence_ref"`
	AccommodationEvidenceRef string        `json:"accommodation_evidence_ref"`
}

// schemaRefWire mirrors [workflow.SchemaRef] field for field. A
// separate type (rather than importing and reusing workflow.SchemaRef
// directly, which this file otherwise could) keeps this wire encoding's
// shape pinned to what WORK-010 stores regardless of any future field
// [workflow.SchemaRef] gains for compiler-internal purposes.
type schemaRefWire struct {
	SchemaID         string `json:"schema_id"`
	Version          uint32 `json:"version"`
	ProtobufFullName string `json:"protobuf_full_name"`
}

// submissionBody encodes s as the JSON body [RecordDecision] stores.
func submissionBody(s Submission) (json.RawMessage, error) {
	w := submissionWire{
		WorkflowInstanceID: s.WorkflowInstanceID.String(),
		NodeID:             s.NodeID,
		WorkItemID:         s.WorkItemID.String(),
		ItemVersion:        s.ItemVersion,
		CompletedBy:        s.CompletedBy,
		CandidateVia:       string(s.CandidateVia),
		DelegationID:       s.DelegationID,
		ClaimID:            s.ClaimID.String(),
		ClaimExpiresAt:     s.ClaimExpiresAt.String(),
		SubmittedAt:        s.SubmittedAt.String(),
		OutputSchema: schemaRefWire{
			SchemaID: s.OutputSchema.SchemaID, Version: s.OutputSchema.Version,
			ProtobufFullName: s.OutputSchema.ProtobufFullName,
		},
		CanonicalPayloadDigest:   s.CanonicalPayloadDigest,
		FormDefinition:           s.FormDefinition,
		RenderContextDigest:      s.RenderContextDigest,
		ValidationEvidenceRef:    s.ValidationEvidenceRef,
		AccessibilityEvidenceRef: s.AccessibilityEvidenceRef,
		AccommodationEvidenceRef: s.AccommodationEvidenceRef,
	}
	raw, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("%w: encode task submission body: %v", ErrInvalidSubmission, err)
	}
	return raw, nil
}

// recordSubmission appends sub's full content as the work item's
// work_item_decision row, in the same transaction as completed's own
// completion. It refuses (through [workitem.RecordDecision]) unless
// sub.Digest() -- the value [Submit] already recorded as completed's
// CompletedOutputDigest -- matches exactly.
func recordSubmission(ctx context.Context, tx workitem.Executor, completed workitem.WorkItem, sub Submission) error {
	body, err := submissionBody(sub)
	if err != nil {
		return err
	}
	_, err = workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
		Item:       completed,
		Kind:       workitem.DecisionKindTask,
		Body:       body,
		BodyDigest: sub.Digest(),
		DecidedBy:  sub.CompletedBy,
		DecidedAt:  sub.SubmittedAt.Time(),
	})
	if err != nil {
		return fmt.Errorf("workflow steps/task: record task submission: %w", err)
	}
	return nil
}
