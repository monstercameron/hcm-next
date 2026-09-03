package execute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

type continuationSink struct {
	tx dbport.Tx

	durable  runtime.ContinuationSink
	factory  WorkItemFactory
	terminal TerminalWriter
	guard    idempotency.Store
	policy   idempotency.RetentionPolicy

	workflowID    string
	planDigest    string
	proposal      runtime.ProposalBinding
	cellID        string
	correlationID string
	startKey      string
	subjectRefs   []string

	created []workitem.WorkItem
}

func (s *continuationSink) RequireWorkItem(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	if err := s.durable.RequireWorkItem(ctx, ex, rec); err != nil {
		return err
	}
	if s.factory == nil {
		return invalid("WORK_ITEM_REQUIRED for node %s but no WorkItemFactory is configured", rec.TargetNodeID)
	}
	id := runtime.ContinuationID(rec.TenantID, rec.InstanceID, rec.SourceNodeID, rec.SourceAttempt, rec.TargetNodeID, rec.Kind)
	item, err := s.factory.CreateAndRoute(ctx, ex, WorkItemRequest{
		WorkItemID: id, Continuation: rec, Proposal: s.proposal,
		CellID: s.cellID, CorrelationID: s.correlationID,
		SubjectRefs: append([]string(nil), s.subjectRefs...), CreatedAt: rec.RecordedAt,
	})
	if err != nil {
		return err
	}
	if item.WorkItemID != id {
		return invalid("WorkItemFactory returned id %s for continuation-derived id %s", item.WorkItemID, id)
	}
	s.created = append(s.created, item)
	return nil
}

func (s *continuationSink) RequireSignalSubscription(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	if err := s.durable.RequireSignalSubscription(ctx, ex, rec); err != nil {
		return err
	}
	return unsupported("SIGNAL_SUBSCRIPTION_REQUIRED", rec.TargetNodeID)
}

func (s *continuationSink) RequireTimer(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	if err := s.durable.RequireTimer(ctx, ex, rec); err != nil {
		return err
	}
	return unsupported("TIMER_REQUIRED", rec.TargetNodeID)
}

func (s *continuationSink) MarkReady(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	return s.durable.MarkReady(ctx, ex, rec)
}

func (s *continuationSink) Complete(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	if err := s.durable.Complete(ctx, ex, rec); err != nil {
		return err
	}
	if s.terminal == nil || s.guard == nil {
		return invalid("COMPLETE for node %s requires TerminalWriter and idempotency Store", rec.TargetNodeID)
	}
	digest := terminalDigest(s.planDigest, s.proposal, rec)
	scope := idempotency.Scope{
		Tenant: rec.TenantID, Capability: s.workflowID,
		EffectScope: "workflow-terminal:" + s.proposal.Revision.ProposalRevisionID,
		Key:         s.startKey,
	}
	_, err := idempotency.Guard(ctx, s.tx, s.guard, scope, digest, s.policy, rec.RecordedAt,
		func(ctx context.Context, tx dbport.Tx) (idempotency.ResultIdentity, error) {
			return s.terminal.Write(ctx, tx, TerminalWriteRequest{
				TenantID: rec.TenantID, InstanceID: rec.InstanceID,
				WorkflowID: s.workflowID, PlanDigest: s.planDigest,
				Proposal: s.proposal, TerminalCode: rec.TerminalCode,
				CorrelationID: s.correlationID, IdempotencyKey: s.startKey,
				RecordedAt: rec.RecordedAt,
			})
		})
	return err
}

func terminalDigest(planDigest string, proposal runtime.ProposalBinding, rec runtime.ContinuationRecord) string {
	material := struct {
		PlanDigest         string `json:"plan_digest"`
		ProposalRevisionID string `json:"proposal_revision_id"`
		ProposalDigest     string `json:"proposal_digest"`
		InstanceID         string `json:"instance_id"`
		TerminalCode       string `json:"terminal_code"`
	}{
		PlanDigest: planDigest, ProposalRevisionID: proposal.Revision.ProposalRevisionID,
		ProposalDigest: proposal.Revision.MaterialDigest.Digest,
		InstanceID:     rec.InstanceID.String(), TerminalCode: rec.TerminalCode,
	}
	b, err := json.Marshal(material)
	if err != nil {
		b = []byte(fmt.Sprintf("unencodable:%v", err))
	}
	h := sha256.New()
	h.Write([]byte("hcmnext.workflow.execute.TerminalWrite/v1"))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

var _ runtime.ContinuationSink = (*continuationSink)(nil)
