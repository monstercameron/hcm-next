package inspect

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Request is one inspection: the already-loaded runtime state and the
// already-evaluated authorization decision to render it under.
//
// The state is an input rather than something this package fetches, which is
// what keeps the inspector read-only by construction: there is no handle here
// it could write through.
type Request struct {
	Instance      runtime.Instance
	Nodes         []runtime.NodeExecution
	Authorization Authorization
}

// Build renders the inspection.
//
// It returns [ErrNotDisclosable] for a caller who may not learn the instance
// exists, and otherwise always returns a complete traversal: every stage in
// [TraversalOrder] appears, a denied stage carries its policy token, a
// protected reference the caller may not see is a redacted [Ref], and anything
// the projection expected but did not receive is named in
// [Completeness.Gaps]. Nothing is silently dropped.
//
// Build performs no I/O, holds no state and mutates neither argument, so it is
// safe to call concurrently over shared inputs.
func Build(req Request) (View, error) {
	auth := req.Authorization
	if err := auth.Validate(); err != nil {
		return View{}, err
	}
	if !auth.InstanceDisclosable {
		return View{}, fmt.Errorf("%w: %s", ErrNotDisclosable, auth.DenialReason)
	}
	if err := req.Instance.Validate(); err != nil {
		return View{}, fmt.Errorf("%w: instance is not storable state: %s", ErrInvalidRequest, err)
	}
	for _, n := range req.Nodes {
		if n.InstanceID != req.Instance.InstanceID || n.TenantID != req.Instance.TenantID {
			return View{}, fmt.Errorf("%w: node execution %s belongs to another instance",
				ErrInvalidRequest, n.NodeExecutionID)
		}
	}

	c := &collector{}
	inst := req.Instance

	view := View{
		PolicyVersion: auth.PolicyVersion,
		Purpose:       auth.Purpose,
		Subject:       auth.Subject,
		Traversal:     TraversalOrder(),
		Definition:    buildDefinition(inst, auth, c),
		Instance:      buildInstance(inst, auth, c),
	}

	// Nodes are rendered in a fixed order (node id, then attempt) so two
	// renderings of the same state are byte-identical.
	nodes := append([]runtime.NodeExecution(nil), req.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].NodeID != nodes[j].NodeID {
			return nodes[i].NodeID < nodes[j].NodeID
		}
		return nodes[i].Attempt < nodes[j].Attempt
	})

	frontier := map[string]bool{}
	for _, id := range inst.CurrentNodeIDs {
		frontier[id] = true
	}

	nodeAllowed := auth.AllowsSection(SectionNode)
	if !nodeAllowed {
		c.redact(string(SectionNode))
	}
	view.Nodes = make([]NodeView, 0, len(nodes))
	if nodeAllowed {
		for _, n := range nodes {
			view.Nodes = append(view.Nodes, buildNode(n, inst, auth, frontier[n.NodeID], c))
		}
	}

	view.Frontier = buildFrontier(inst, nodes, nodeAllowed, c)
	view.Completeness = c.completeness()
	return view, nil
}

// collector accumulates the redactions and gaps a build discovers, so that
// every place that withholds or misses something is recorded in exactly one
// way.
type collector struct {
	redactions map[string]bool
	gaps       map[string]bool
}

func (c *collector) redact(token string) {
	if c.redactions == nil {
		c.redactions = map[string]bool{}
	}
	c.redactions[token] = true
}

func (c *collector) gap(format string, args ...any) {
	if c.gaps == nil {
		c.gaps = map[string]bool{}
	}
	c.gaps[fmt.Sprintf(format, args...)] = true
}

func (c *collector) completeness() Completeness {
	return Completeness{
		Complete:   len(c.redactions) == 0 && len(c.gaps) == 0,
		Redactions: sortedKeys(c.redactions),
		Gaps:       sortedKeys(c.gaps),
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildDefinition(inst runtime.Instance, auth Authorization, c *collector) DefinitionView {
	if !auth.AllowsSection(SectionDefinition) {
		c.redact(string(SectionDefinition))
		return DefinitionView{Disclosed: false, DeniedReason: auth.sectionReason(SectionDefinition)}
	}
	return DefinitionView{
		Disclosed:        true,
		WorkflowID:       inst.WorkflowID,
		WorkflowVersion:  inst.WorkflowVersion,
		CompiledPlanHash: inst.CompiledPlanHash,
	}
}

func buildInstance(inst runtime.Instance, auth Authorization, c *collector) InstanceView {
	if !auth.AllowsSection(SectionInstance) {
		c.redact(string(SectionInstance))
		return InstanceView{
			Disclosed:           false,
			DeniedReason:        auth.sectionReason(SectionInstance),
			InputRef:            RefRedacted(auth.sectionReason(SectionInstance)),
			EffectiveContextRef: RefRedacted(auth.sectionReason(SectionInstance)),
			LastCheckpointRef:   RefRedacted(auth.sectionReason(SectionInstance)),
			BusinessSubjectRefs: RefListRedacted(auth.sectionReason(SectionInstance)),
			Lifecycle:           dimensionsOf(runtime.Dimensions{}),
		}
	}
	if inst.CompletionDimensions.Empty() && inst.RuntimeStatus.Terminal() {
		// A terminal instance with no dimensions recorded is a hole in the
		// evidence, not a neutral default: the whole point of the five
		// dimensions is that a runtime status does not imply them.
		c.gap("instance.completion_dimensions")
	}
	out := InstanceView{
		Disclosed:            true,
		InstanceID:           inst.InstanceID.String(),
		CellID:               inst.CellID,
		CorrelationID:        inst.CorrelationID,
		ExecutionMode:        string(inst.ExecutionMode),
		RuntimeStatus:        string(inst.RuntimeStatus),
		InstanceVersion:      inst.InstanceVersion,
		VariableRevisionHead: inst.VariableRevisionHead,
		Lifecycle:            dimensionsOf(inst.CompletionDimensions),
		LastCheckpointRef:    RefValue(inst.LastCheckpointRef),
		CreatedAt:            inst.CreatedAt,
		StartedAt:            inst.StartedAt,
		CompletedAt:          inst.CompletedAt,
	}
	out.InputRef = protectedRef(inst.InputRef, FieldInstanceInput, SectionInstance, auth, c)
	out.EffectiveContextRef = protectedRef(inst.EffectiveContextRef, FieldInstanceContext, SectionInstance, auth, c)
	if auth.AllowsField(FieldInstanceSubjects, SectionInstance) {
		out.BusinessSubjectRefs = RefListValue(inst.BusinessSubjectRefs)
	} else {
		c.redact(FieldInstanceSubjects)
		out.BusinessSubjectRefs = RefListRedacted(auth.fieldReason(FieldInstanceSubjects, SectionInstance))
	}
	return out
}

// buildFrontier renders the instance's current nodes. A frontier node with no
// recorded execution is rendered with AttemptRecorded false and recorded as a
// gap: an inspector that showed the frontier as empty, or omitted the node,
// would be hiding the exact condition an operator is looking for.
func buildFrontier(
	inst runtime.Instance, nodes []runtime.NodeExecution, nodeAllowed bool, c *collector,
) []FrontierEntry {
	latest := map[string]runtime.NodeExecution{}
	for _, n := range nodes {
		if prev, ok := latest[n.NodeID]; !ok || n.Attempt > prev.Attempt {
			latest[n.NodeID] = n
		}
	}
	out := make([]FrontierEntry, 0, len(inst.CurrentNodeIDs))
	for _, id := range inst.CurrentNodeIDs {
		entry := FrontierEntry{NodeID: id}
		n, ok := latest[id]
		switch {
		case !ok:
			c.gap("frontier.%s: no node execution recorded", id)
		case !nodeAllowed:
			// The node stage is denied, so the attempt detail is withheld;
			// the frontier node id itself is instance state, not node state.
			entry.AttemptRecorded = false
		default:
			entry.AttemptRecorded = true
			entry.Attempt = n.Attempt
			entry.Status = string(n.Status)
			entry.StepType = string(n.StepType)
		}
		out = append(out, entry)
	}
	return out
}

func buildNode(
	n runtime.NodeExecution, inst runtime.Instance, auth Authorization, current bool, c *collector,
) NodeView {
	out := NodeView{
		NodeID:         n.NodeID,
		Attempt:        n.Attempt,
		StepType:       string(n.StepType),
		Status:         string(n.Status),
		Current:        current,
		RetryPolicyRef: RefValue(n.Refs.RetryPolicyRef),
		StartedAt:      n.StartedAt,
		CompletedAt:    n.CompletedAt,
		RecordedAt:     n.RecordedAt,
	}
	out.InputSnapshotRef = protectedRef(n.InputSnapshotRef, FieldNodeInput, SectionNode, auth, c)
	out.OutputArtifactRef = protectedRef(n.OutputArtifactRef, FieldNodeOutput, SectionNode, auth, c)

	if auth.AllowsSection(SectionGovernance) {
		out.Governance = GovernanceView{
			Disclosed:               true,
			AuthorizationDecisionID: RefValue(n.Refs.AuthorizationDecisionID),
			DecisionID:              RefValue(n.Refs.DecisionID),
			PolicyRef:               RefValue(n.Refs.PolicyRef),
			ProposalRef:             RefValue(n.Refs.ProposalRef),
			BaselineRef:             RefValue(n.Refs.BaselineRef),
			HumanTaskID:             RefValue(n.Refs.HumanTaskID),
			AgentExecutionID:        RefValue(n.Refs.AgentExecutionID),
		}
	} else {
		reason := auth.sectionReason(SectionGovernance)
		c.redact(string(SectionGovernance))
		out.Governance = GovernanceView{
			Disclosed:               false,
			DeniedReason:            reason,
			AuthorizationDecisionID: RefRedacted(reason),
			DecisionID:              RefRedacted(reason),
			PolicyRef:               RefRedacted(reason),
			ProposalRef:             RefRedacted(reason),
			BaselineRef:             RefRedacted(reason),
			HumanTaskID:             RefRedacted(reason),
			AgentExecutionID:        RefRedacted(reason),
		}
	}

	if auth.AllowsSection(SectionTransaction) {
		txRef := ""
		if inst.BusinessTransactionID != nil {
			txRef = inst.BusinessTransactionID.String()
		}
		out.Transaction = TransactionView{Disclosed: true, BusinessTransactionID: RefValue(txRef)}
	} else {
		reason := auth.sectionReason(SectionTransaction)
		c.redact(string(SectionTransaction))
		out.Transaction = TransactionView{
			Disclosed: false, DeniedReason: reason, BusinessTransactionID: RefRedacted(reason),
		}
	}

	if auth.AllowsSection(SectionConnector) {
		out.Connector = ConnectorView{
			Disclosed:             true,
			CapabilityExecutionID: RefValue(n.Refs.CapabilityExecutionID),
			EffectRefs:            RefListValue(n.Refs.EffectRefs),
		}
	} else {
		reason := auth.sectionReason(SectionConnector)
		c.redact(string(SectionConnector))
		out.Connector = ConnectorView{
			Disclosed:             false,
			DeniedReason:          reason,
			CapabilityExecutionID: RefRedacted(reason),
			EffectRefs:            RefListRedacted(reason),
		}
	}

	if auth.AllowsSection(SectionObservation) {
		out.Observation = ObservationView{
			Disclosed:  true,
			ErrorClass: RefValue(n.ErrorClass),
			RepairRef:  RefValue(n.Refs.RepairRef),
		}
	} else {
		reason := auth.sectionReason(SectionObservation)
		c.redact(string(SectionObservation))
		out.Observation = ObservationView{
			Disclosed: false, DeniedReason: reason,
			ErrorClass: RefRedacted(reason), RepairRef: RefRedacted(reason),
		}
	}

	if auth.AllowsSection(SectionTrace) {
		out.Trace = TraceView{Disclosed: true, TraceID: RefValue(n.TraceID)}
	} else {
		reason := auth.sectionReason(SectionTrace)
		c.redact(string(SectionTrace))
		out.Trace = TraceView{Disclosed: false, DeniedReason: reason, TraceID: RefRedacted(reason)}
	}

	// A failed attempt that names no error class cannot be diagnosed, and an
	// unexplained failure is exactly the thing an inspector must not swallow.
	if n.Status == runtime.NodeFailed && n.ErrorClass == "" && auth.AllowsSection(SectionObservation) {
		c.gap("node.%s attempt %d: FAILED with no error class", n.NodeID, n.Attempt)
	}
	return out
}

// protectedRef renders one reference that points at separately authorized
// content, redacting it (and recording the redaction) when the decision says
// so.
func protectedRef(value, field string, section Section, auth Authorization, c *collector) Ref {
	if auth.AllowsField(field, section) {
		return RefValue(value)
	}
	c.redact(field)
	return RefRedacted(auth.fieldReason(field, section))
}
