package frontier

import (
	"sort"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// NodeState is the durable state of one node execution. The vocabulary is the
// one the runtime spec declares (planning/specs/workflow-runtime.md, "Durable
// Node Execution"): READY -> RUNNING -> SUCCEEDED, with WAITING, FAILED ->
// RETRYING -> READY, and the settled exceptional states beside them.
type NodeState string

// The declared node states.
const (
	// NodeReady is scheduled work a worker may claim.
	NodeReady NodeState = "READY"
	// NodeRunning is claimed work with a live attempt.
	NodeRunning NodeState = "RUNNING"
	// NodeWaiting is durable suspension: a work item, a signal or a timer.
	NodeWaiting NodeState = "WAITING"
	// NodeRetrying is a failed attempt inside its declared retry budget.
	NodeRetrying NodeState = "RETRYING"

	// NodeSucceeded is a completed attempt. A node succeeds when it produced a
	// typed outcome, including a degraded one: REJECTED is a result, not a
	// failure of the step to run.
	NodeSucceeded NodeState = "SUCCEEDED"
	// NodeFailed is an attempt that produced no outcome and exhausted retry.
	NodeFailed NodeState = "FAILED"
	// NodeSkipped is a node an explicit route excluded.
	NodeSkipped NodeState = "SKIPPED"
	// NodeOverridden is a node an authorized exception discharged.
	NodeOverridden NodeState = "OVERRIDDEN"
	// NodeCompensated is a node whose effects were countered.
	NodeCompensated NodeState = "COMPENSATED"
	// NodeCancelled is a node cancellation policy stopped.
	NodeCancelled NodeState = "CANCELLED"
)

// Active reports whether a node in this state is still on the frontier.
func (s NodeState) Active() bool {
	switch s {
	case NodeReady, NodeRunning, NodeWaiting, NodeRetrying:
		return true
	default:
		return false
	}
}

// Valid reports whether s names a declared node state.
func (s NodeState) Valid() bool {
	switch s {
	case NodeReady, NodeRunning, NodeWaiting, NodeRetrying,
		NodeSucceeded, NodeFailed, NodeSkipped, NodeOverridden,
		NodeCompensated, NodeCancelled:
		return true
	default:
		return false
	}
}

// NodeStatus is one node's snapshot: its state, how many attempts it has cost,
// the route its last completion took and the digest of the output that
// completion produced.
//
// It is a value with no reference fields on purpose. A snapshot handed to
// [Advance] cannot be mutated by the caller mid-computation, and a snapshot
// read back out of a runtime row reproduces the same transition.
type NodeStatus struct {
	NodeID       string    `json:"node_id"`
	State        NodeState `json:"state"`
	Attempts     uint32    `json:"attempts"`
	RouteKey     string    `json:"route_key,omitempty"`
	OutputDigest string    `json:"output_digest,omitempty"`
}

// JoinStrategy is one declared completion semantic of a JOIN
// (planning/workflows/_engine/step-types.md, "JOIN").
type JoinStrategy string

// The declared join strategies.
const (
	// JoinAll activates when every declared branch has succeeded.
	JoinAll JoinStrategy = "ALL"
	// JoinAny activates on the first successful branch.
	JoinAny JoinStrategy = "ANY"
	// JoinQuorum activates when a declared count of branches has succeeded.
	JoinQuorum JoinStrategy = "QUORUM"
	// JoinRequiredSet activates when every branch in a declared required set
	// has succeeded, whatever the others did.
	JoinRequiredSet JoinStrategy = "REQUIRED_SET"
	// JoinBestEffort activates once every branch is accounted for — succeeded,
	// failed or skipped — and never reports the missing ones as success.
	JoinBestEffort JoinStrategy = "BEST_EFFORT"
)

// Valid reports whether s names a declared join strategy.
func (s JoinStrategy) Valid() bool {
	switch s {
	case JoinAll, JoinAny, JoinQuorum, JoinRequiredSet, JoinBestEffort:
		return true
	default:
		return false
	}
}

// JoinDeclaration is the strategy an instance is started with for one JOIN
// node. It is supplied to [Seed] rather than inferred at advancement time: a
// runtime that guessed ANY where the author meant ALL would turn a missing
// mandatory branch into success, which is exactly what step-types.md forbids.
type JoinDeclaration struct {
	NodeID   string       `json:"node_id"`
	Strategy JoinStrategy `json:"strategy"`
	// RequiredCount is the number of successful branches QUORUM needs. It is
	// meaningless for the other strategies.
	RequiredCount uint32 `json:"required_count,omitempty"`
	// RequiredBranches names the branches REQUIRED_SET cannot do without. It
	// is meaningless for the other strategies.
	RequiredBranches []string `json:"required_branches,omitempty"`
}

// JoinCounter is one JOIN's arrival ledger. It records which declared branches
// arrived, which of those succeeded and which were skipped, so an activation
// decision is auditable rather than a bare integer.
type JoinCounter struct {
	NodeID   string       `json:"node_id"`
	Strategy JoinStrategy `json:"strategy"`
	// Required is the number of successful arrivals the strategy needs.
	Required uint32 `json:"required"`
	// RequiredBranches is REQUIRED_SET's declared mandatory branch set.
	RequiredBranches []string `json:"required_branches,omitempty"`
	// Branches are every declared incoming branch, sorted.
	Branches []string `json:"branches"`
	// Arrived, Satisfied and Skipped partition the branches seen so far.
	// Satisfied is a subset of Arrived; Skipped is disjoint from both.
	Arrived   []string `json:"arrived,omitempty"`
	Satisfied []string `json:"satisfied,omitempty"`
	Skipped   []string `json:"skipped,omitempty"`
	// Activated reports whether the strategy's condition has been met.
	Activated bool `json:"activated"`
	// Blocked reports that every branch is accounted for and the strategy can
	// no longer be met. It is stated rather than silently waiting forever.
	Blocked bool `json:"blocked"`
}

func (j JoinCounter) clone() JoinCounter {
	j.RequiredBranches = append([]string(nil), j.RequiredBranches...)
	j.Branches = append([]string(nil), j.Branches...)
	j.Arrived = append([]string(nil), j.Arrived...)
	j.Satisfied = append([]string(nil), j.Satisfied...)
	j.Skipped = append([]string(nil), j.Skipped...)
	return j
}

// LifecycleState is the five-dimension intent lifecycle state a terminal
// declared, spelled as canonical state ids. There is no sixth dimension and no
// collapsed status.
type LifecycleState struct {
	RequestState     string `json:"request_state"`
	ExecutionState   string `json:"execution_state"`
	BusinessState    string `json:"business_state"`
	ConsistencyState string `json:"consistency_state"`
	ObligationState  string `json:"obligation_state"`
}

func lifecycleStateOf(d lifecycle.Dimensions) LifecycleState {
	return LifecycleState{
		RequestState:     string(d.State(lifecycle.DimensionRequest)),
		ExecutionState:   string(d.State(lifecycle.DimensionExecution)),
		BusinessState:    string(d.State(lifecycle.DimensionBusiness)),
		ConsistencyState: string(d.State(lifecycle.DimensionConsistency)),
		ObligationState:  string(d.State(lifecycle.DimensionObligation)),
	}
}

// TerminalRecord is the terminal an instance reached, restated as values so
// reading the state does not require re-reading the plan.
type TerminalRecord struct {
	NodeID                    string                 `json:"node_id"`
	TerminalCode              string                 `json:"terminal_code"`
	RuntimeStatus             workflow.RuntimeStatus `json:"runtime_status"`
	Lifecycle                 LifecycleState         `json:"lifecycle"`
	OutstandingObligationRefs []string               `json:"outstanding_obligation_refs,omitempty"`
	RepairRefs                []string               `json:"repair_refs,omitempty"`
	IncidentRefs              []string               `json:"incident_refs,omitempty"`
}

func (t TerminalRecord) clone() TerminalRecord {
	t.OutstandingObligationRefs = append([]string(nil), t.OutstandingObligationRefs...)
	t.RepairRefs = append([]string(nil), t.RepairRefs...)
	t.IncidentRefs = append([]string(nil), t.IncidentRefs...)
	return t
}

// terminalRecordOf restates a compiled terminal artifact as a record.
func terminalRecordOf(t workflow.Terminal) TerminalRecord {
	return TerminalRecord{
		NodeID:                    t.NodeID,
		TerminalCode:              t.TerminalCode,
		RuntimeStatus:             t.RuntimeStatus,
		Lifecycle:                 lifecycleStateOf(t.Dimensions),
		OutstandingObligationRefs: append([]string(nil), t.OutstandingObligationRefs...),
		RepairRefs:                append([]string(nil), t.RepairRefs...),
		IncidentRefs:              append([]string(nil), t.IncidentRefs...),
	}
}

// InstanceState is a value snapshot of one workflow instance: which plan it is
// pinned to, which nodes are on the frontier, what state every node it has
// touched is in, every JOIN's arrival ledger, and the terminal it reached if
// it reached one.
//
// Every field is a value or a slice of values, and every accessor and every
// [Advance] result deep-copies, so no caller ever holds a reference into
// another caller's snapshot.
type InstanceState struct {
	InstanceID string `json:"instance_id"`
	WorkflowID string `json:"workflow_id"`
	Version    uint32 `json:"version"`
	// PlanDigest pins the exact compiled plan this state was derived against.
	// [Advance] refuses a plan whose digest disagrees.
	PlanDigest string `json:"plan_digest"`
	// Sequence counts advancements. It makes two otherwise identical states
	// distinguishable and gives the digest a monotonic component.
	Sequence uint64 `json:"sequence"`

	// Frontier is every node id whose state is active, sorted. It is derived
	// from Nodes rather than tracked beside it, so the two cannot disagree.
	Frontier []string `json:"frontier"`
	// Nodes is every node the instance has touched, sorted by node id.
	Nodes []NodeStatus `json:"nodes"`
	// Joins is one counter per JOIN node the plan declares, sorted by node id.
	Joins []JoinCounter `json:"joins,omitempty"`

	Completed bool           `json:"completed"`
	Terminal  TerminalRecord `json:"terminal,omitzero"`
}

// Clone returns a deep copy. It is exported because a caller persisting a
// state and a caller advancing it must not share backing arrays.
func (s InstanceState) Clone() InstanceState {
	s.Frontier = append([]string(nil), s.Frontier...)
	s.Nodes = append([]NodeStatus(nil), s.Nodes...)
	joins := make([]JoinCounter, len(s.Joins))
	for i, j := range s.Joins {
		joins[i] = j.clone()
	}
	s.Joins = joins
	s.Terminal = s.Terminal.clone()
	return s
}

// Node returns one node's status and whether the instance has touched it.
func (s InstanceState) Node(id string) (NodeStatus, bool) {
	i := sort.Search(len(s.Nodes), func(i int) bool { return s.Nodes[i].NodeID >= id })
	if i < len(s.Nodes) && s.Nodes[i].NodeID == id {
		return s.Nodes[i], true
	}
	return NodeStatus{}, false
}

// Join returns one JOIN's counter and whether the plan declared it.
func (s InstanceState) Join(id string) (JoinCounter, bool) {
	i := sort.Search(len(s.Joins), func(i int) bool { return s.Joins[i].NodeID >= id })
	if i < len(s.Joins) && s.Joins[i].NodeID == id {
		return s.Joins[i].clone(), true
	}
	return JoinCounter{}, false
}

// setNode inserts or replaces one node status, keeping the slice sorted.
func (s *InstanceState) setNode(st NodeStatus) {
	i := sort.Search(len(s.Nodes), func(i int) bool { return s.Nodes[i].NodeID >= st.NodeID })
	if i < len(s.Nodes) && s.Nodes[i].NodeID == st.NodeID {
		s.Nodes[i] = st
		return
	}
	s.Nodes = append(s.Nodes, NodeStatus{})
	copy(s.Nodes[i+1:], s.Nodes[i:])
	s.Nodes[i] = st
}

// setJoin replaces one join counter, keeping the slice sorted.
func (s *InstanceState) setJoin(j JoinCounter) {
	i := sort.Search(len(s.Joins), func(i int) bool { return s.Joins[i].NodeID >= j.NodeID })
	if i < len(s.Joins) && s.Joins[i].NodeID == j.NodeID {
		s.Joins[i] = j
		return
	}
	s.Joins = append(s.Joins, JoinCounter{})
	copy(s.Joins[i+1:], s.Joins[i:])
	s.Joins[i] = j
}

// recomputeFrontier derives the frontier from the node states. The frontier is
// never edited directly, so it cannot drift away from the states it summarizes.
func (s *InstanceState) recomputeFrontier() {
	out := make([]string, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		if n.State.Active() {
			out = append(out, n.NodeID)
		}
	}
	sort.Strings(out)
	s.Frontier = out
}

// Seed builds the initial state of one instance: the plan's start node READY,
// one arrival ledger per declared JOIN, and nothing else.
//
// It belongs here rather than in the instance service because the join
// declarations are part of what makes advancement deterministic, and because a
// state nobody can construct without a database is a state no test can pin.
func Seed(plan *workflow.CompiledWorkflow, instanceID string, decls ...JoinDeclaration) (InstanceState, error) {
	if plan == nil {
		return InstanceState{}, refuse(CodeInvalidPlan, "", "no compiled plan")
	}
	start, ok := plan.Node(plan.StartNodeID)
	if !ok {
		return InstanceState{}, refuse(CodeInvalidPlan, plan.StartNodeID,
			"plan %s declares a start node it does not carry", plan.WorkflowID)
	}
	joins, err := seedJoins(plan, decls)
	if err != nil {
		return InstanceState{}, err
	}
	state := InstanceState{
		InstanceID: instanceID,
		WorkflowID: plan.WorkflowID,
		Version:    plan.Version,
		PlanDigest: plan.Digest(),
		Nodes:      []NodeStatus{{NodeID: start.ID, State: NodeReady}},
		Joins:      joins,
	}
	state.recomputeFrontier()
	return state, nil
}

// seedJoins builds one counter per JOIN node. The branch set is the plan's own
// incoming edges; the strategy is the caller's declaration, defaulting to ALL
// — the only strategy that cannot turn a missing branch into success.
func seedJoins(plan *workflow.CompiledWorkflow, decls []JoinDeclaration) ([]JoinCounter, error) {
	declared := make(map[string]JoinDeclaration, len(decls))
	for _, d := range decls {
		node, ok := plan.Node(d.NodeID)
		if !ok {
			return nil, refuse(CodeInvalidJoinDeclaration, d.NodeID, "plan declares no such node")
		}
		if node.Type != workflow.StepJoin {
			return nil, refuse(CodeInvalidJoinDeclaration, d.NodeID,
				"join strategy declared for a %s node", node.Type)
		}
		if _, dup := declared[d.NodeID]; dup {
			return nil, refuse(CodeInvalidJoinDeclaration, d.NodeID,
				"two join strategies declared for one node")
		}
		if !d.Strategy.Valid() {
			return nil, refuse(CodeInvalidJoinDeclaration, d.NodeID,
				"strategy %q is not a declared join strategy", d.Strategy)
		}
		declared[d.NodeID] = d
	}

	var out []JoinCounter
	for _, node := range plan.Nodes {
		if node.Type != workflow.StepJoin {
			continue
		}
		branches := incomingBranches(plan, node.ID)
		decl, ok := declared[node.ID]
		if !ok {
			decl = JoinDeclaration{NodeID: node.ID, Strategy: JoinAll}
		}
		counter, err := newJoinCounter(decl, branches)
		if err != nil {
			return nil, err
		}
		out = append(out, counter)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

func newJoinCounter(d JoinDeclaration, branches []string) (JoinCounter, error) {
	c := JoinCounter{
		NodeID:   d.NodeID,
		Strategy: d.Strategy,
		Branches: branches,
	}
	switch d.Strategy {
	case JoinAll:
		c.Required = uint32(len(branches))
	case JoinAny:
		if len(branches) == 0 {
			return JoinCounter{}, refuse(CodeInvalidJoinDeclaration, d.NodeID,
				"ANY needs at least one declared branch")
		}
		c.Required = 1
	case JoinQuorum:
		if d.RequiredCount == 0 || int(d.RequiredCount) > len(branches) {
			return JoinCounter{}, refuse(CodeInvalidJoinDeclaration, d.NodeID,
				"QUORUM requires a count in 1..%d, got %d", len(branches), d.RequiredCount)
		}
		c.Required = d.RequiredCount
	case JoinRequiredSet:
		if len(d.RequiredBranches) == 0 {
			return JoinCounter{}, refuse(CodeInvalidJoinDeclaration, d.NodeID,
				"REQUIRED_SET declares no required branch")
		}
		set := append([]string(nil), d.RequiredBranches...)
		sort.Strings(set)
		for _, b := range set {
			if !contains(branches, b) {
				return JoinCounter{}, refuse(CodeInvalidJoinDeclaration, d.NodeID,
					"required branch %q is not an incoming branch of this JOIN", b)
			}
		}
		c.RequiredBranches = set
		c.Required = uint32(len(set))
	case JoinBestEffort:
		c.Required = 0
	}
	return c, nil
}

// incomingBranches lists the distinct source nodes of a node's incoming edges,
// sorted. It is the declared branch set of a JOIN: the compiler already proved
// every one of those edges is explicitly routed.
func incomingBranches(plan *workflow.CompiledWorkflow, id string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, e := range plan.Edges {
		if e.To != id || seen[e.From] {
			continue
		}
		seen[e.From] = true
		out = append(out, e.From)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// insertSorted adds s to a sorted, deduplicated list.
func insertSorted(list []string, s string) []string {
	if contains(list, s) {
		return list
	}
	list = append(list, s)
	sort.Strings(list)
	return list
}
