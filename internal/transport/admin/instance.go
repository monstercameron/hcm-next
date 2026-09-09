package admin

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// GetWorkflowInstance is ADMIN-008's governed, read-only execution inspector:
// it asks the application-side [app.WorkflowInstanceReader] for the
// instance, its recorded node executions, its work items and their
// transitions, then hands the loaded rows to internal/workflow/inspect.Build
// and inspect.BuildWorkItems, which do the actual rendering and redaction.
// This method reads no table, writes nothing, and evaluates no HCM business
// rule; it composes one read port and two pure projections.
func (s *server) GetWorkflowInstance(ctx context.Context, req *adminv1.GetWorkflowInstanceRequest) (*adminv1.GetWorkflowInstanceResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	evidence := envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}
	// Request shape is validated before this method looks at whether its own
	// optional port is configured: a malformed request must fail the same
	// way regardless of composition, and an unconfigured port must never be
	// what a caller learns about their own request shape.
	instanceID, parseErr := runtime.ParseUUID(req.GetInstanceId())
	if parseErr != nil {
		return nil, envelope.New(envelope.CodeInvalidArgument,
			"admin.instance_id_required", "instance_id must be a valid uuid").
			WithViolation("instance_id", "must be a valid uuid", "inspect.Build").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}
	if s.deps.WorkflowInstances == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.workflow_instance_unconfigured",
			"the workflow runtime read port is not configured").
			WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	record, err := s.deps.WorkflowInstances.ReadWorkflowInstance(ctx, principal.Tenant(), instanceID)
	if err != nil {
		if runtime.CodeOf(err) == runtime.CodeInstanceNotFound {
			return nil, envelope.New(envelope.CodeNotFound,
				"admin.workflow_instance_not_found", "no such workflow instance").
				WithCorrelation(inv.RequestID()).WithEvidence(evidence)
		}
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.workflow_instance_load_failed", "the workflow instance could not be read").
			WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}

	view, err := inspect.Build(inspect.Request{
		Instance:      record.Instance,
		Nodes:         record.Nodes,
		Authorization: adminpolicy.OperatorWorkflowInstanceAuthorization(principal.Subject()),
	})
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.workflow_instance_build_failed", "the workflow instance could not be projected").
			WithDiagnostic(err).WithCorrelation(inv.RequestID()).WithEvidence(evidence)
	}
	workItems := inspect.BuildWorkItems(record.WorkItems, record.Transitions, adminpolicy.OperatorWorkItemAuthorization())

	resp := &adminv1.GetWorkflowInstanceResponse{
		Disclosed:             true,
		Definition:            toWorkflowDefinitionProfile(view.Definition),
		Instance:              toWorkflowInstanceProfile(view.Instance),
		Complete:              view.Completeness.Complete && workItems.Completeness.Complete,
		Redactions:            mergeSorted(view.Completeness.Redactions, workItems.Completeness.Redactions),
		Gaps:                  mergeSorted(view.Completeness.Gaps, workItems.Completeness.Gaps),
		WorkItemsDisclosed:    workItems.Disclosed,
		WorkItemsDeniedReason: workItems.DeniedReason,
		EvidenceRef:           &commonv1.EvidenceRef{EvidenceId: principal.EvidenceID(), EvidenceKind: "admin.get_workflow_instance"},
	}
	for _, f := range view.Frontier {
		resp.Frontier = append(resp.Frontier, &adminv1.FrontierEntryProfile{
			NodeId:          f.NodeID,
			AttemptRecorded: f.AttemptRecorded,
			Attempt:         int32(f.Attempt),
			Status:          f.Status,
			StepType:        f.StepType,
		})
	}
	for _, n := range view.Nodes {
		resp.Nodes = append(resp.Nodes, toNodeProfile(n))
	}
	for _, w := range workItems.Items {
		resp.WorkItems = append(resp.WorkItems, toWorkItemProfile(w))
	}
	return resp, nil
}

// mergeSorted concatenates two already-sorted string slices into one sorted,
// de-duplicated slice, so a caller reading the response's redactions or gaps
// sees one ordered list rather than two lists it must merge itself.
func mergeSorted(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			out = append(out, a[i])
			i++
		default:
			out = append(out, b[j])
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

func toRefValue(r inspect.Ref) *adminv1.RefValue {
	v, ok := r.Get()
	out := &adminv1.RefValue{State: r.State().String(), GapKind: string(inspect.RefGapKind(r))}
	if ok {
		out.Value = v
	} else {
		out.Reason = r.Reason()
	}
	return out
}

func toRefListValue(l inspect.RefList) *adminv1.RefListValue {
	return &adminv1.RefListValue{
		State:  l.State.String(),
		Values: append([]string(nil), l.Values...),
		Reason: l.Reason,
	}
}

func toTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func toOptionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return toTimestamp(*t)
}

func toWorkflowDefinitionProfile(d inspect.DefinitionView) *adminv1.WorkflowDefinitionProfile {
	return &adminv1.WorkflowDefinitionProfile{
		Disclosed:        d.Disclosed,
		DeniedReason:     d.DeniedReason,
		WorkflowId:       d.WorkflowID,
		WorkflowVersion:  d.WorkflowVersion,
		CompiledPlanHash: d.CompiledPlanHash,
	}
}

func toLifecycleProfile(l inspect.Lifecycle) *adminv1.LifecycleProfile {
	return &adminv1.LifecycleProfile{
		RequestState:     l.RequestState,
		ExecutionState:   l.ExecutionState,
		BusinessState:    l.BusinessState,
		ConsistencyState: l.ConsistencyState,
		ObligationState:  l.ObligationState,
	}
}

func toWorkflowInstanceProfile(i inspect.InstanceView) *adminv1.WorkflowInstanceProfile {
	return &adminv1.WorkflowInstanceProfile{
		Disclosed:            i.Disclosed,
		DeniedReason:         i.DeniedReason,
		InstanceId:           i.InstanceID,
		CellId:               i.CellID,
		CorrelationId:        i.CorrelationID,
		ExecutionMode:        i.ExecutionMode,
		RuntimeStatus:        i.RuntimeStatus,
		InstanceVersion:      i.InstanceVersion,
		VariableRevisionHead: i.VariableRevisionHead,
		Lifecycle:            toLifecycleProfile(i.Lifecycle),
		InputRef:             toRefValue(i.InputRef),
		EffectiveContextRef:  toRefValue(i.EffectiveContextRef),
		LastCheckpointRef:    toRefValue(i.LastCheckpointRef),
		BusinessSubjectRefs:  toRefListValue(i.BusinessSubjectRefs),
		CreatedAt:            toTimestamp(i.CreatedAt),
		StartedAt:            toOptionalTimestamp(i.StartedAt),
		CompletedAt:          toOptionalTimestamp(i.CompletedAt),
	}
}

func toGovernanceProfile(g inspect.GovernanceView) *adminv1.GovernanceProfile {
	return &adminv1.GovernanceProfile{
		Disclosed:               g.Disclosed,
		DeniedReason:            g.DeniedReason,
		AuthorizationDecisionId: toRefValue(g.AuthorizationDecisionID),
		DecisionId:              toRefValue(g.DecisionID),
		PolicyRef:               toRefValue(g.PolicyRef),
		ProposalRef:             toRefValue(g.ProposalRef),
		BaselineRef:             toRefValue(g.BaselineRef),
		HumanTaskId:             toRefValue(g.HumanTaskID),
		AgentExecutionId:        toRefValue(g.AgentExecutionID),
	}
}

func toTransactionProfile(t inspect.TransactionView) *adminv1.TransactionProfile {
	return &adminv1.TransactionProfile{
		Disclosed:             t.Disclosed,
		DeniedReason:          t.DeniedReason,
		BusinessTransactionId: toRefValue(t.BusinessTransactionID),
	}
}

func toConnectorProfile(c inspect.ConnectorView) *adminv1.ConnectorProfile {
	return &adminv1.ConnectorProfile{
		Disclosed:             c.Disclosed,
		DeniedReason:          c.DeniedReason,
		CapabilityExecutionId: toRefValue(c.CapabilityExecutionID),
		EffectRefs:            toRefListValue(c.EffectRefs),
	}
}

func toObservationProfile(o inspect.ObservationView) *adminv1.ObservationProfile {
	return &adminv1.ObservationProfile{
		Disclosed:    o.Disclosed,
		DeniedReason: o.DeniedReason,
		ErrorClass:   toRefValue(o.ErrorClass),
		RepairRef:    toRefValue(o.RepairRef),
	}
}

func toTraceProfile(t inspect.TraceView) *adminv1.TraceProfile {
	return &adminv1.TraceProfile{
		Disclosed:    t.Disclosed,
		DeniedReason: t.DeniedReason,
		TraceId:      toRefValue(t.TraceID),
	}
}

func toNodeProfile(n inspect.NodeView) *adminv1.NodeProfile {
	return &adminv1.NodeProfile{
		NodeId:            n.NodeID,
		Attempt:           int32(n.Attempt),
		StepType:          n.StepType,
		Status:            n.Status,
		Current:           n.Current,
		RetryPolicyRef:    toRefValue(n.RetryPolicyRef),
		InputSnapshotRef:  toRefValue(n.InputSnapshotRef),
		OutputArtifactRef: toRefValue(n.OutputArtifactRef),
		Governance:        toGovernanceProfile(n.Governance),
		Transaction:       toTransactionProfile(n.Transaction),
		Connector:         toConnectorProfile(n.Connector),
		Observation:       toObservationProfile(n.Observation),
		Trace:             toTraceProfile(n.Trace),
		StartedAt:         toOptionalTimestamp(n.StartedAt),
		CompletedAt:       toOptionalTimestamp(n.CompletedAt),
		RecordedAt:        toTimestamp(n.RecordedAt),
	}
}

func toWorkItemTransitionProfile(t inspect.TransitionView) *adminv1.WorkItemTransitionProfile {
	return &adminv1.WorkItemTransitionProfile{
		TransitionId:     t.TransitionID,
		ItemVersion:      t.ItemVersion,
		FromStatus:       t.FromStatus,
		ToStatus:         t.ToStatus,
		ActorPrincipalId: t.ActorPrincipalID,
		Reason:           t.Reason,
		Detail:           t.Detail,
		EvidenceRef:      toRefValue(t.EvidenceRef),
		At:               toTimestamp(t.At),
		RecordedAt:       toTimestamp(t.RecordedAt),
	}
}

func toWorkItemProfile(w inspect.WorkItemView) *adminv1.WorkItemProfile {
	out := &adminv1.WorkItemProfile{
		WorkItemId:             w.WorkItemID,
		ItemVersion:            w.ItemVersion,
		Kind:                   w.Kind,
		WorkType:               w.WorkType,
		Status:                 w.Status,
		CorrelationId:          w.CorrelationID,
		NodeId:                 w.NodeID,
		ApprovalRequirementRef: toRefValue(w.ApprovalRequirementRef),
		ProposalRef:            toRefValue(w.ProposalRef),
		SubjectRefs:            toRefListValue(w.SubjectRefs),
		OwnerKind:              w.OwnerKind,
		OwnerRef:               w.OwnerRef,
		PolicyRouteRef:         w.PolicyRouteRef,
		Visibility:             w.Visibility,
		DeadlineAt:             toTimestamp(w.DeadlineAt),
		ClaimedBy:              w.ClaimedBy,
		CompletedBy:            w.CompletedBy,
		CompletedOutputDigest:  toRefValue(w.CompletedOutputDigest),
		CreatedAt:              toTimestamp(w.CreatedAt),
		TransitionsRecorded:    w.TransitionsRecorded,
	}
	for _, t := range w.Transitions {
		out.Transitions = append(out.Transitions, toWorkItemTransitionProfile(t))
	}
	return out
}
