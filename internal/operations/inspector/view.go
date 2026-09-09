// Package inspector: workflow view construction (ADMIN-002).
package inspector

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Gated field identifiers. A caller building the [authz.Request] that feeds
// [BuildWorkflowView]'s decision includes whichever of these it wants ruled
// on; a field absent from the decision's ruling set - exactly like
// authz.FieldDecision.Covers documents - is treated as a refusal to answer,
// not an implicit allow.
const (
	// FieldExecutionTrace gates per-node status, attempt, evidence
	// references and failure reason.
	FieldExecutionTrace authz.FieldID = "operations.inspector.execution_trace"
	// FieldHumanWork gates the pending human work list, which names
	// assignees.
	FieldHumanWork authz.FieldID = "operations.inspector.human_work"
)

// DimensionsView renders the intent kernel's five lifecycle dimensions as
// their stable string IDs, matching lifecycle.Dimensions.String's own
// per-dimension names.
type DimensionsView struct {
	Request     string
	Execution   string
	Business    string
	Consistency string
	Obligation  string
}

func dimensionsView(d lifecycle.Dimensions) DimensionsView {
	return DimensionsView{
		Request:     string(d.Request.StateID()),
		Execution:   string(d.Execution.StateID()),
		Business:    string(d.Business.StateID()),
		Consistency: string(d.Consistency.StateID()),
		Obligation:  string(d.Obligation.StateID()),
	}
}

// TerminalView is the redaction-free part of a compiled END node: its
// declared terminal identity and lifecycle dimensions. A terminal's
// dimensions are a compiled fact of the plan itself (WF-STEP-017), not
// execution evidence, so - unlike NodeView's status/evidence fields - it is
// never gated by [FieldExecutionTrace].
type TerminalView struct {
	TerminalCode  string
	RuntimeStatus string
	Dimensions    DimensionsView
}

// NodeView is one compiled node's inspector-facing projection: its plan
// shape (always visible) beside its execution overlay (gated).
type NodeView struct {
	ID          string
	Type        string
	Depth       uint32
	SafePoint   bool
	EffectClass string
	Governance  struct {
		Purpose           string
		RequiredDecisions []string
	}
	Terminal *TerminalView

	// Status/Attempt/EvidenceRefs/FailureReason are the execution overlay,
	// gated by FieldExecutionTrace. Status is StatusUnknown, and the rest
	// are empty, whenever the trace has nothing for this node or the
	// decision withholds it.
	Status        Status
	Attempt       uint32
	EvidenceRefs  []string
	FailureReason string
}

// EdgeView is one compiled edge: the plan's routing shape, never gated.
type EdgeView struct {
	From     string
	To       string
	RouteKey string
}

// EffectSummaryView mirrors workflow.EffectSummary for display.
type EffectSummaryView struct {
	ZeroEffect        bool
	NodesByClass      map[string][]string
	EffectKeys        []string
	IrreversibleNodes []string
}

// WorkflowView is the ADMIN-002 execution inspector projection: a compiled
// plan's node/edge graph with per-node execution status, safe points, the
// plan's effect summary, pending human work and a content digest.
type WorkflowView struct {
	WorkflowID      string
	Version         uint32
	Phase           string
	TerminalProfile string
	PlanDigest      string
	StartNodeID     string

	Nodes   []NodeView
	Edges   []EdgeView
	Effects EffectSummaryView

	// PendingHumanWork is gated by FieldHumanWork; nil when withheld.
	PendingHumanWork []HumanWorkItem

	// Withheld is true when the decision's subject was not disclosable at
	// all: every gated field above is empty/nil regardless of individual
	// field rulings, matching authz.Decision's own "withhold every field
	// uniformly" convention for a non-disclosable subject.
	Withheld bool

	// Digest is this view's own content digest: two BuildWorkflowView calls
	// with the same plan, trace and decision always produce the same
	// Digest, and any observable difference in the returned view changes
	// it.
	Digest string
}

// gateOpen reports whether field is affirmatively allowed by dec. A nil
// decision means "unrestricted" (an internal/system caller that has already
// authorized itself upstream); any other decision must carry an explicit
// ALLOW ruling for field, exactly like authz.FieldDecision.Covers expects
// callers to treat a missing ruling as a refusal to answer.
func gateOpen(dec *authz.Decision, field authz.FieldID) bool {
	if dec == nil {
		return true
	}
	if !dec.SubjectDisclosable {
		return false
	}
	ruling, ok := dec.Fields[field]
	return ok && ruling.Effect == authz.EffectAllow
}

func withheldAll(dec *authz.Decision) bool {
	return dec != nil && !dec.SubjectDisclosable
}

// BuildWorkflowView projects plan and trace into one authorization-gated
// [WorkflowView]. A nil trace is treated as [SimulationReceipt]{}: every
// node reports StatusUnknown and no human work is pending. A nil dec is
// treated as unrestricted (see [gateOpen]).
func BuildWorkflowView(plan *workflow.CompiledWorkflow, trace ExecutionTrace, dec *authz.Decision) (WorkflowView, error) {
	if plan == nil {
		return WorkflowView{}, fmt.Errorf("inspector: BuildWorkflowView requires a non-nil compiled plan")
	}
	if trace == nil {
		trace = emptyTrace{}
	}

	traceOpen := gateOpen(dec, FieldExecutionTrace)
	humanWorkOpen := gateOpen(dec, FieldHumanWork)

	view := WorkflowView{
		WorkflowID:      plan.WorkflowID,
		Version:         plan.Version,
		Phase:           string(plan.Phase),
		TerminalProfile: string(plan.TerminalProfile),
		PlanDigest:      plan.Digest(),
		StartNodeID:     plan.StartNodeID,
		Withheld:        withheldAll(dec),
		Effects: EffectSummaryView{
			ZeroEffect:        plan.Effects.ZeroEffect,
			NodesByClass:      cloneStringSliceMap(plan.Effects.NodesByClass),
			EffectKeys:        append([]string(nil), plan.Effects.EffectKeys...),
			IrreversibleNodes: append([]string(nil), plan.Effects.IrreversibleNodes...),
		},
	}

	nodes := make([]NodeView, 0, len(plan.Nodes))
	for _, n := range plan.Nodes {
		nodes = append(nodes, buildNodeView(n, trace, traceOpen))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	view.Nodes = nodes

	edges := make([]EdgeView, 0, len(plan.Edges))
	for _, e := range plan.Edges {
		edges = append(edges, EdgeView{From: e.From, To: e.To, RouteKey: e.RouteKey})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].RouteKey < edges[j].RouteKey
	})
	view.Edges = edges

	if humanWorkOpen {
		view.PendingHumanWork = append([]HumanWorkItem(nil), trace.PendingHumanWork()...)
	}

	view.Digest = digestView(view)
	return view, nil
}

func cloneStringSliceMap(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func buildNodeView(n workflow.CompiledNode, trace ExecutionTrace, traceOpen bool) NodeView {
	nv := NodeView{
		ID:          n.ID,
		Type:        string(n.Type),
		Depth:       n.Depth,
		SafePoint:   n.SafePoint,
		EffectClass: string(n.EffectClass),
		Status:      StatusUnknown,
	}
	nv.Governance.Purpose = n.Governance.Purpose
	nv.Governance.RequiredDecisions = make([]string, 0, len(n.Governance.RequiredDecisions))
	for _, d := range n.Governance.RequiredDecisions {
		nv.Governance.RequiredDecisions = append(nv.Governance.RequiredDecisions, string(d))
	}

	if n.Terminal != nil {
		tv := TerminalView{
			TerminalCode:  n.Terminal.TerminalCode,
			RuntimeStatus: string(n.Terminal.RuntimeStatus),
			Dimensions:    dimensionsView(n.Terminal.Dimensions),
		}
		nv.Terminal = &tv
	}

	if !traceOpen {
		return nv
	}

	ev, ok := trace.NodeEvidence(n.ID)
	if !ok {
		return nv
	}
	nv.Status = ev.Status
	if !nv.Status.Valid() {
		nv.Status = StatusUnknown
	}
	nv.Attempt = ev.Attempt
	nv.EvidenceRefs = append([]string(nil), ev.EvidenceRefs...)
	nv.FailureReason = ev.FailureReason
	return nv
}

// digestView computes WorkflowView.Digest deterministically from every
// observable field of view (post-redaction), so the digest itself can never
// leak more than the view already exposes.
func digestView(v WorkflowView) string {
	b := viewdigest.New().
		String("workflow_id", v.WorkflowID).
		Uint("version", uint64(v.Version)).
		String("phase", v.Phase).
		String("terminal_profile", v.TerminalProfile).
		String("plan_digest", v.PlanDigest).
		String("start_node_id", v.StartNodeID).
		Bool("withheld", v.Withheld).
		Bool("effects.zero_effect", v.Effects.ZeroEffect).
		SortedStrings("effects.effect_keys", v.Effects.EffectKeys).
		SortedStrings("effects.irreversible_nodes", v.Effects.IrreversibleNodes)

	classKeys := make([]string, 0, len(v.Effects.NodesByClass))
	for k := range v.Effects.NodesByClass {
		classKeys = append(classKeys, k)
	}
	sort.Strings(classKeys)
	b.Int("effects.classes", int64(len(classKeys)))
	for _, k := range classKeys {
		b.String("effects.class", k).SortedStrings("effects.class_nodes", v.Effects.NodesByClass[k])
	}

	b.Int("nodes", int64(len(v.Nodes)))
	for _, n := range v.Nodes {
		b.String("node.id", n.ID).
			String("node.type", n.Type).
			Uint("node.depth", uint64(n.Depth)).
			Bool("node.safe_point", n.SafePoint).
			String("node.effect_class", n.EffectClass).
			String("node.governance.purpose", n.Governance.Purpose).
			SortedStrings("node.governance.required_decisions", n.Governance.RequiredDecisions).
			String("node.status", string(n.Status)).
			Uint("node.attempt", uint64(n.Attempt)).
			SortedStrings("node.evidence_refs", n.EvidenceRefs).
			String("node.failure_reason", n.FailureReason)
		if n.Terminal != nil {
			t := n.Terminal
			b.Bool("node.terminal.present", true).
				String("node.terminal.code", t.TerminalCode).
				String("node.terminal.runtime_status", t.RuntimeStatus).
				String("node.terminal.dim.request", t.Dimensions.Request).
				String("node.terminal.dim.execution", t.Dimensions.Execution).
				String("node.terminal.dim.business", t.Dimensions.Business).
				String("node.terminal.dim.consistency", t.Dimensions.Consistency).
				String("node.terminal.dim.obligation", t.Dimensions.Obligation)
		} else {
			b.Bool("node.terminal.present", false)
		}
	}

	b.Int("edges", int64(len(v.Edges)))
	for _, e := range v.Edges {
		b.String("edge.from", e.From).String("edge.to", e.To).String("edge.route", e.RouteKey)
	}

	b.Int("pending_human_work", int64(len(v.PendingHumanWork)))
	for _, w := range v.PendingHumanWork {
		b.String("work.node_id", w.NodeID).
			String("work.kind", w.Kind).
			String("work.assigned_to", w.AssignedTo).
			String("work.requested_at", w.RequestedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"))
	}

	return b.Digest()
}
