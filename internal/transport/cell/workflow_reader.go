package cell

import (
	"context"
	"strconv"

	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transport/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// workflowReader adapts the application-owned durable workflow reader to the
// transport's deliberately smaller, redaction-safe inspection port. The
// adapter owns no queries and makes no authorization decisions; it only
// converts application records into the generated-boundary projection.
type workflowReader struct {
	source app.WorkflowInstanceReader
}

var _ workflow.Reader = workflowReader{}

func newWorkflowReader(source app.WorkflowInstanceReader) workflow.Reader {
	if source == nil {
		return nil
	}
	return workflowReader{source: source}
}

func (r workflowReader) ReadWorkflowInstance(ctx context.Context, tenant, instanceID string) (workflow.Record, error) {
	id, err := runtime.ParseUUID(instanceID)
	if err != nil {
		return workflow.Record{}, workflow.ErrNotFound
	}
	record, err := r.source.ReadWorkflowInstance(ctx, values.TenantId(tenant), id)
	if err != nil {
		if runtime.CodeOf(err) == runtime.CodeInstanceNotFound {
			return workflow.Record{}, workflow.ErrNotFound
		}
		return workflow.Record{}, err
	}
	return convertWorkflowRecord(record), nil
}

func convertWorkflowRecord(record app.WorkflowInstanceRecord) workflow.Record {
	instance := record.Instance
	out := workflow.Instance{
		InstanceID:          instance.InstanceID.String(),
		TenantID:            instance.TenantID.String(),
		CellID:              instance.CellID,
		WorkflowID:          instance.WorkflowID,
		WorkflowVersion:     instance.WorkflowVersion,
		CompiledPlanDigest:  instance.CompiledPlanHash,
		BusinessSubjectRefs: append([]string(nil), instance.BusinessSubjectRefs...),
		ExecutionMode:       string(instance.ExecutionMode),
		RuntimeStatus:       string(instance.RuntimeStatus),
		RequestState:        instance.CompletionDimensions.RequestState,
		ExecutionState:      instance.CompletionDimensions.ExecutionState,
		BusinessState:       instance.CompletionDimensions.BusinessState,
		ConsistencyState:    instance.CompletionDimensions.ConsistencyState,
		ObligationState:     instance.CompletionDimensions.ObligationState,
		InputRef:            instance.InputRef,
		CurrentNodeIDs:      append([]string(nil), instance.CurrentNodeIDs...),
		EffectiveContextRef: instance.EffectiveContextRef,
		LastCheckpointRef:   instance.LastCheckpointRef,
		InstanceVersion:     uint64(instance.InstanceVersion),
		CorrelationID:       instance.CorrelationID,
		CreatedAt:           instance.CreatedAt,
		StartedAt:           instance.StartedAt,
		CompletedAt:         instance.CompletedAt,
	}
	if instance.BusinessTransactionID != nil {
		out.BusinessTransactionID = instance.BusinessTransactionID.String()
	}
	if instance.VariableRevisionHead != 0 {
		out.VariableRevisionHead = strconv.FormatInt(instance.VariableRevisionHead, 10)
	}

	result := workflow.Record{Instance: out, Nodes: make([]workflow.NodeExecution, 0, len(record.Nodes))}
	for _, node := range record.Nodes {
		result.Nodes = append(result.Nodes, workflow.NodeExecution{
			NodeExecutionID:         node.NodeExecutionID.String(),
			WorkflowInstanceID:      node.InstanceID.String(),
			NodeID:                  node.NodeID,
			Attempt:                 uint32(node.Attempt),
			Status:                  string(node.Status),
			InputSnapshotRef:        node.InputSnapshotRef,
			OutputArtifactRef:       node.OutputArtifactRef,
			CapabilityExecutionID:   node.Refs.CapabilityExecutionID,
			AuthorizationDecisionID: node.Refs.AuthorizationDecisionID,
			DecisionID:              node.Refs.DecisionID,
			HumanTaskID:             node.Refs.HumanTaskID,
			ErrorClass:              node.ErrorClass,
			StartedAt:               node.StartedAt,
			CompletedAt:             node.CompletedAt,
			TraceID:                 node.TraceID,
		})
	}
	return result
}
