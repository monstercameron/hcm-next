package frontier

import (
	"sort"

	"github.com/monstercameron/hcm-next/internal/workflow"
)

// IntentKind names one thing a runtime must persist as a result of an
// advancement. This is the whole scheduling vocabulary: there is no
// general-purpose "do something" intent, because an intent a reader cannot
// classify is an intent an operator cannot audit.
type IntentKind string

// The declared scheduling intents.
const (
	// IntentReady says a node execution must be created in READY state and
	// published to the ready queue.
	IntentReady IntentKind = "READY"
	// IntentWorkItemRequired says a governed WorkItem must be created for an
	// APPROVAL or TASK node. This package resolves no assignee and grants no
	// authority; internal/humanwork does that.
	IntentWorkItemRequired IntentKind = "WORK_ITEM_REQUIRED"
	// IntentSignalSubscriptionRequired says a correlated signal subscription
	// must be registered before the instance can be woken by the event.
	IntentSignalSubscriptionRequired IntentKind = "SIGNAL_SUBSCRIPTION_REQUIRED"
	// IntentTimerRequired says a durable timer must be created. No executor
	// sleeps; the scheduler owns the timer.
	IntentTimerRequired IntentKind = "TIMER_REQUIRED"
	// IntentComplete says the instance reached its terminal and must be
	// closed with the stated lifecycle dimensions.
	IntentComplete IntentKind = "COMPLETE"
)

// Valid reports whether k names a declared scheduling intent.
func (k IntentKind) Valid() bool {
	switch k {
	case IntentReady, IntentWorkItemRequired, IntentSignalSubscriptionRequired,
		IntentTimerRequired, IntentComplete:
		return true
	default:
		return false
	}
}

// Intent is one scheduling record a runtime must persist inside the same
// transaction as the state change that derived it. It is data: this package
// creates no work item, registers no subscription and enqueues nothing.
type Intent struct {
	Kind   IntentKind        `json:"kind"`
	NodeID string            `json:"node_id"`
	Type   workflow.StepType `json:"type,omitempty"`
	// RouteKey is the outcome route that produced this intent, empty for an
	// intent no route produced (a retry, or an await raised in place).
	RouteKey string `json:"route_key,omitempty"`
	// Ref is the correlation an awaiting intent needs — the requirement,
	// event type or wake condition the handler named. Opaque here.
	Ref string `json:"ref,omitempty"`
	// TerminalCode is set on IntentComplete only.
	TerminalCode string `json:"terminal_code,omitempty"`
}

// Successor is one node the advancement activated, changed or settled, with
// the state it now holds.
type Successor struct {
	NodeID string            `json:"node_id"`
	Type   workflow.StepType `json:"type"`
	State  NodeState         `json:"state"`
	// RouteKey is the edge that reached it, empty when no edge did.
	RouteKey string `json:"route_key,omitempty"`
	// ViaDefault reports that a DECISION's declared default_route selected
	// this successor because the evaluator produced an undeclared key. It is
	// recorded because "the default was applied" is a materially different
	// fact from "the route matched".
	ViaDefault bool `json:"via_default,omitempty"`
	// ViaFailure reports that the node's declared failure_route selected this
	// successor.
	ViaFailure bool `json:"via_failure,omitempty"`
	// JoinPending reports a JOIN successor whose declared strategy is not yet
	// met. It carries no intent, so it activates no work.
	JoinPending bool `json:"join_pending,omitempty"`
}

// Transition is the complete, deterministic result of one advancement: which
// node completed and in what state, which successors it activated, which
// branches it excluded, every join counter it moved, the terminal it reached
// if it reached one, the scheduling intents a runtime must persist, and the
// instance state that results.
//
// It is a value with a content digest. Two evaluations of [Advance] over the
// same plan, state and outcome produce the same digest, which is the property
// WF-RUN-024 exists to establish.
type Transition struct {
	InstanceID string `json:"instance_id"`
	WorkflowID string `json:"workflow_id"`
	Version    uint32 `json:"version"`
	PlanDigest string `json:"plan_digest"`
	// Sequence is the resulting state's sequence number.
	Sequence uint64 `json:"sequence"`

	// CompletedNodeID and CompletedState are the node this advancement was
	// driven by and the state it now holds.
	CompletedNodeID string            `json:"completed_node_id"`
	CompletedType   workflow.StepType `json:"completed_type"`
	CompletedState  NodeState         `json:"completed_state"`
	// RouteKey is the outcome route the completion took, empty when the node
	// did not complete on an outcome.
	RouteKey string `json:"route_key,omitempty"`
	// OutputDigest is the completed node's typed output digest, recorded
	// verbatim from the outcome.
	OutputDigest string `json:"output_digest,omitempty"`

	// Successors are the nodes this advancement activated or moved, sorted by
	// node id.
	Successors []Successor `json:"successors,omitempty"`
	// Skipped names the branches the taken route excluded, sorted. They are
	// settled SKIPPED and carry no intent.
	Skipped []string `json:"skipped,omitempty"`
	// Joins are the join counters this advancement changed, sorted.
	Joins []JoinCounter `json:"joins,omitempty"`

	// Complete and Terminal report a reached terminal. Terminal is meaningful
	// only when Complete is set.
	Complete bool           `json:"complete"`
	Terminal TerminalRecord `json:"terminal,omitzero"`

	// Intents are the scheduling records a runtime must persist, sorted by
	// kind then node id.
	Intents []Intent `json:"intents,omitempty"`
	// Frontier is the resulting frontier, sorted. It restates Next.Frontier so
	// a reader does not have to open the state to see what is still open.
	Frontier []string `json:"frontier"`
	// Next is the resulting instance state.
	Next InstanceState `json:"next"`

	digest string
}

// Digest is the transition's content identity. Repeated evaluation of the same
// advancement reproduces it exactly.
func (t Transition) Digest() string { return t.digest }

// Verify recomputes the digest over the transition's current content and
// reports whether it still matches the digest minted by [Advance].
func (t Transition) Verify() error {
	if got := computeTransitionDigest(t); got != t.digest {
		return refuse(CodeInvalidState, t.CompletedNodeID,
			"transition content no longer matches its digest")
	}
	return nil
}

// Intent returns the intent this transition carries for a node, if any.
func (t Transition) Intent(nodeID string) (Intent, bool) {
	for _, i := range t.Intents {
		if i.NodeID == nodeID {
			return i, true
		}
	}
	return Intent{}, false
}

// SuccessorIDs lists the activated successor node ids in sorted order.
func (t Transition) SuccessorIDs() []string {
	out := make([]string, 0, len(t.Successors))
	for _, s := range t.Successors {
		out = append(out, s.NodeID)
	}
	return out
}

// sortIntents orders intents by kind then node id, so an intent set is a set
// rather than an artifact of evaluation order.
func sortIntents(in []Intent) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Kind != in[j].Kind {
			return in[i].Kind < in[j].Kind
		}
		return in[i].NodeID < in[j].NodeID
	})
}

// sortSuccessors orders successors by node id.
func sortSuccessors(in []Successor) {
	sort.SliceStable(in, func(i, j int) bool { return in[i].NodeID < in[j].NodeID })
}
