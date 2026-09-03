package approval

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent"
	intentapproval "github.com/monstercameron/hcm-next/internal/intent/approval"
)

// decisionBindingWire and decisionWire are WORK-010's JSON encoding of one
// [intentapproval.ApprovalDecision], minted for the work_item_decision row
// [Complete] records in the same transaction as the item's own completion.
//
// It is a dedicated wire struct, not a direct json.Marshal(decision), for the
// same reason internal/humanwork/workitem/assignment.go declares one for
// [humanwork.Resolution]: [intentapproval.ApprovalDecision.DecidedAt] is a
// [values.Instant], whose type carries no exported fields at all, so
// encoding/json's default reflection produces an empty object for it. Every
// instant here is carried as its canonical RFC 3339 text instead, which keeps
// the encoding total and keeps the field order fixed by this struct's
// declaration.
type decisionBindingWire struct {
	RequirementID              string                  `json:"requirement_id"`
	RequirementRevision        uint64                  `json:"requirement_revision"`
	IntentID                   string                  `json:"intent_id"`
	ProposalRevisionID         string                  `json:"proposal_revision_id"`
	ProposalDigest             digest.Reference        `json:"proposal_digest"`
	TaskVersion                uint64                  `json:"task_version"`
	RenderedProjectionDigest   string                  `json:"rendered_projection_digest"`
	ControlSnapshots           intent.ControlSnapshots `json:"control_snapshots"`
	RequirementDigest          string                  `json:"requirement_digest"`
	ResolutionExpressionDigest string                  `json:"resolution_expression_digest"`
}

type decisionApproverWire struct {
	PrincipalID          string `json:"principal_id"`
	IdentityAssuranceRef string `json:"identity_assurance_ref"`
	SessionRef           string `json:"session_ref"`
	Via                  string `json:"via"`
	DelegationID         string `json:"delegation_id,omitempty"`
}

type decisionWire struct {
	DecisionID           string               `json:"decision_id"`
	Binding              decisionBindingWire  `json:"binding"`
	Outcome              string               `json:"outcome"`
	Approver             decisionApproverWire `json:"approver"`
	AuthorityDecisionRef string               `json:"authority_decision_ref"`
	Reason               string               `json:"reason"`
	DecidedAt            string               `json:"decided_at"`
	VoteDigest           string               `json:"vote_digest"`
}

// decisionBody encodes d as the JSON body [RecordDecision] stores. It is
// total over every well-formed [intentapproval.ApprovalDecision]: the only
// failure mode is one the type system already forecloses (json.Marshal on
// plain strings, uints and nested plain-field structs never errors), so the
// error return exists only so a future field addition that breaks that
// property fails loudly instead of silently.
func decisionBody(d intentapproval.ApprovalDecision) (json.RawMessage, error) {
	w := decisionWire{
		DecisionID: d.DecisionID,
		Binding: decisionBindingWire{
			RequirementID:              d.Binding.RequirementID,
			RequirementRevision:        d.Binding.RequirementRevision,
			IntentID:                   d.Binding.IntentID,
			ProposalRevisionID:         d.Binding.ProposalRevisionID,
			ProposalDigest:             d.Binding.ProposalDigest,
			TaskVersion:                d.Binding.TaskVersion,
			RenderedProjectionDigest:   d.Binding.RenderedProjectionDigest,
			ControlSnapshots:           d.Binding.ControlSnapshots,
			RequirementDigest:          d.Binding.RequirementDigest,
			ResolutionExpressionDigest: d.Binding.ResolutionExpressionDigest,
		},
		Outcome: string(d.Outcome),
		Approver: decisionApproverWire{
			PrincipalID:          d.Approver.PrincipalID,
			IdentityAssuranceRef: d.Approver.IdentityAssuranceRef,
			SessionRef:           d.Approver.SessionRef,
			Via:                  string(d.Approver.Via),
			DelegationID:         d.Approver.DelegationID,
		},
		AuthorityDecisionRef: d.AuthorityDecisionRef,
		Reason:               d.Reason,
		DecidedAt:            d.DecidedAt.String(),
		VoteDigest:           d.VoteDigest,
	}
	raw, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("%w: encode approval decision body: %v", ErrInvalidEvidence, err)
	}
	return raw, nil
}

// recordDecision appends decision's full content as the work item's
// work_item_decision row, in the same transaction as completed's own
// completion. It refuses (through [workitem.RecordDecision]) unless
// decision.Digest() -- the value Complete already recorded as completed's
// CompletedOutputDigest -- matches exactly, so the row this call inserts can
// never disagree with the digest work_item itself commits to.
func recordDecision(
	ctx context.Context, tx workitem.Executor, completed workitem.WorkItem, decision intentapproval.ApprovalDecision,
) error {
	body, err := decisionBody(decision)
	if err != nil {
		return err
	}
	_, err = workitem.RecordDecision(ctx, tx, workitem.RecordDecisionInput{
		Item:       completed,
		Kind:       workitem.DecisionKindApproval,
		Body:       body,
		BodyDigest: decision.Digest(),
		DecidedBy:  decision.Approver.PrincipalID,
		DecidedAt:  decision.DecidedAt.Time(),
	})
	if err != nil {
		return fmt.Errorf("workflow steps/approval: record approval decision: %w", err)
	}
	return nil
}
