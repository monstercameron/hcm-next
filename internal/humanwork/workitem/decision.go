// Package workitem: this file is WORK-010. RecordDecision persists the full
// typed approval decision or task submission content [steps/approval.Complete]
// and [steps/task.Submit] already digest into a work item's immutable
// completed_output_digest, so that content is recoverable from storage after
// the run rather than existing only as a hash. LoadDecision reads it back.
package workitem

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// DecisionKind names which typed artifact a work_item_decision row carries.
// It is the same two-value vocabulary [Kind] declares, because a decision
// record only ever exists for a work item of that kind.
type DecisionKind string

// Declared decision kinds.
const (
	// DecisionKindApproval records an intent/approval.ApprovalDecision.
	DecisionKindApproval DecisionKind = "APPROVAL"
	// DecisionKindTask records a steps/task.Submission.
	DecisionKindTask DecisionKind = "TASK"
)

// Valid reports whether k is a declared decision kind.
func (k DecisionKind) Valid() bool {
	return k == DecisionKindApproval || k == DecisionKindTask
}

// DecisionRecord is one row of work_item_decision: the recoverable evidence
// migrations/00022_work_item_decision.sql exists to store, immutable once
// inserted.
type DecisionRecord struct {
	TenantID           uuid.UUID
	DecisionID         uuid.UUID
	WorkItemID         uuid.UUID
	WorkflowInstanceID uuid.UUID
	ItemVersion        int64

	Kind DecisionKind

	// Body is the full typed decision or submission, encoded exactly as the
	// minting package produced it.
	Body json.RawMessage
	// BodyDigest is the digest Body reproduces. It always equals the work
	// item's own completed_output_digest -- [RecordDecision] refuses to
	// insert a row where it does not.
	BodyDigest string

	DecidedBy string
	DecidedAt time.Time

	RecordedAt time.Time
}

// RecordDecisionInput is [RecordDecision]'s request.
type RecordDecisionInput struct {
	// DecisionID is minted here when the zero value.
	DecisionID uuid.UUID

	// Item is the just-completed work item -- typically the exact value
	// [Store.Complete] returned in the same transaction. RecordDecision
	// derives the item's tenant, identity, instance and version from it and
	// refuses a body digest that does not match Item.CompletedOutputDigest,
	// so this table can never disagree with the digest work_item itself
	// commits to.
	Item WorkItem

	Kind DecisionKind
	// Body is the full typed decision or submission, already encoded by the
	// minting package (steps/approval or steps/task): this package does not
	// know either type and never re-derives their content.
	Body json.RawMessage
	// BodyDigest is the digest the minting package computed over the same
	// content it used as Item's CompletedOutputDigest.
	BodyDigest string

	DecidedBy string
	DecidedAt time.Time
}

const decisionColumns = `tenant_id, decision_id, work_item_id, workflow_instance_id, item_version,
	kind, decision_body, decision_body_digest, decided_by, decided_at, recorded_at`

// RecordDecision appends the full typed decision or submission content for a
// just-completed work item, in the caller's own transaction -- the same
// transaction as the [Store.Complete] call that produced in.Item, never a
// later one.
//
// It refuses a body whose digest disagrees with in.Item.CompletedOutputDigest
// ([CodeInvalidRecord]), an item that is not COMPLETED, and a kind that does
// not match the item's own [Kind]. A second RecordDecision for the same work
// item collides on work_item_decision's primary key rather than overwriting
// the first -- a work item completes exactly once, so there is never a
// second decision to record for it.
func RecordDecision(ctx context.Context, ex Executor, in RecordDecisionInput) (DecisionRecord, error) {
	item := in.Item
	id := item.WorkItemID.String()
	if item.WorkItemID == uuid.Nil || item.TenantID == uuid.Nil || item.WorkflowInstanceID == uuid.Nil {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "a decision record requires the completed work item's identity")
	}
	if item.Status != StatusCompleted || item.CompletedOutputDigest == "" {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "work item %s is not COMPLETED; a decision may only be recorded for a completed item", id)
	}
	if !in.Kind.Valid() {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "decision kind %q is not declared", string(in.Kind))
	}
	wantKind := DecisionKindApproval
	if item.Kind == KindTask {
		wantKind = DecisionKindTask
	}
	if in.Kind != wantKind {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id,
			"work item %s is kind %s, which records a %s decision, not %s", id, item.Kind, wantKind, in.Kind)
	}
	if !semanticKey(in.DecidedBy) {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "a decision record must name who decided")
	}
	if in.DecidedAt.IsZero() {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "a decision record must carry when it was decided; this package never reads a wall clock")
	}
	if len(in.Body) == 0 {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "a decision record must carry its body")
	}
	if !json.Valid(in.Body) {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "decision body is not valid JSON")
	}
	if !ValidDigest(in.BodyDigest) {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id, "decision body digest %q is malformed", in.BodyDigest)
	}
	if in.BodyDigest != item.CompletedOutputDigest {
		return DecisionRecord{}, refuse(CodeInvalidRecord, id,
			"decision body digest %q does not match work item %s's completed output digest %q",
			in.BodyDigest, id, item.CompletedOutputDigest)
	}

	decisionID := in.DecisionID
	if decisionID == uuid.Nil {
		decisionID = uuid.New()
	}

	row := ex.QueryRow(ctx, `
		INSERT INTO work_item_decision (`+decisionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		RETURNING `+decisionColumns,
		item.TenantID, decisionID, item.WorkItemID, item.WorkflowInstanceID, item.ItemVersion,
		string(in.Kind), []byte(in.Body), in.BodyDigest, in.DecidedBy, in.DecidedAt.UTC())
	rec, err := scanDecision(row)
	if err != nil {
		return DecisionRecord{}, wrap(CodeStorageFailed, id, err, "insert work item decision")
	}
	return rec, nil
}

// LoadDecision reads the decision record for one work item. A missing row
// and one belonging to another tenant are the same answer,
// [CodeWorkItemNotFound]: this package never discloses that a decision it
// may not see exists.
func LoadDecision(ctx context.Context, ex Executor, tenantID, workItemID uuid.UUID) (DecisionRecord, error) {
	row := ex.QueryRow(ctx,
		`SELECT `+decisionColumns+` FROM work_item_decision WHERE tenant_id = $1 AND work_item_id = $2`,
		tenantID, workItemID)
	rec, err := scanDecision(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return DecisionRecord{}, refuse(CodeWorkItemNotFound, workItemID.String(), "no decision recorded for this work item")
		}
		return DecisionRecord{}, wrap(CodeStorageFailed, workItemID.String(), err, "read work item decision")
	}
	return rec, nil
}

// LoadDecisionsForInstance reads every decision recorded for one workflow
// instance, ordered by recorded_at -- the evidence WF-RUN-030's terminal
// write reads to name approval decision ids and task submission ids
// alongside the promotion outcome.
func LoadDecisionsForInstance(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) ([]DecisionRecord, error) {
	rows, err := ex.Query(ctx,
		`SELECT `+decisionColumns+` FROM work_item_decision
		 WHERE tenant_id = $1 AND workflow_instance_id = $2
		 ORDER BY recorded_at, work_item_id`,
		tenantID, instanceID)
	if err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), err, "list work item decisions for instance")
	}
	defer rows.Close()

	out := []DecisionRecord{}
	for rows.Next() {
		rec, scanErr := scanDecision(rows)
		if scanErr != nil {
			return nil, wrap(CodeStorageFailed, instanceID.String(), scanErr, "scan work item decision")
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap(CodeStorageFailed, instanceID.String(), err, "iterate work item decisions")
	}
	return out, nil
}

func scanDecision(row dbport.Row) (DecisionRecord, error) {
	var (
		rec       DecisionRecord
		kind      string
		bodyRaw   []byte
		decidedAt time.Time
	)
	err := row.Scan(
		&rec.TenantID, &rec.DecisionID, &rec.WorkItemID, &rec.WorkflowInstanceID, &rec.ItemVersion,
		&kind, &bodyRaw, &rec.BodyDigest, &rec.DecidedBy, &decidedAt, &rec.RecordedAt)
	if err != nil {
		return DecisionRecord{}, err
	}
	rec.Kind = DecisionKind(kind)
	rec.Body = json.RawMessage(bodyRaw)
	rec.DecidedAt = decidedAt.UTC()
	rec.RecordedAt = rec.RecordedAt.UTC()
	return rec, nil
}
