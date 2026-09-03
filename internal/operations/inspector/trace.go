package inspector

import "time"

// Status is one node's execution status as surfaced by an [ExecutionTrace].
// [StatusUnknown] is the typed unavailable state: it is what a caller sees
// both when the trace has genuinely not reached a node yet and when
// authorization has withheld the answer, so "no evidence" and "denied
// evidence" are never confused with a false negative such as an empty
// string.
type Status string

const (
	// StatusUnknown is the zero value: no trace evidence is available, or
	// authorization withheld it.
	StatusUnknown Status = "UNKNOWN"
	// StatusPending is a node the trace has not yet reached.
	StatusPending Status = "PENDING"
	// StatusRunning is a node currently executing.
	StatusRunning Status = "RUNNING"
	// StatusSucceeded is a node that produced a SUCCEEDED/PASS-class outcome.
	StatusSucceeded Status = "SUCCEEDED"
	// StatusFailed is a node that produced a FAILED/REJECTED/FAIL-class
	// outcome.
	StatusFailed Status = "FAILED"
	// StatusSkipped is a node no execution path reached because a route
	// upstream of it was not taken.
	StatusSkipped Status = "SKIPPED"
)

// Valid reports whether s is one of the declared statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusUnknown, StatusPending, StatusRunning, StatusSucceeded, StatusFailed, StatusSkipped:
		return true
	default:
		return false
	}
}

// NodeEvidence is the per-node fact an [ExecutionTrace] reports: a status,
// which attempt produced it, the evidence references a node detail view
// would link to, and - for a failed node - the failure reason. This is
// deliberately the minimal shape WF-RUN-019's future node_execution/attempt
// tables need to satisfy; it carries no raw request/response payload, only
// references, so a trace source never has to decide what is safe to expose
// beyond identifying where the real evidence lives.
type NodeEvidence struct {
	NodeID        string
	Status        Status
	Attempt       uint32
	EvidenceRefs  []string
	FailureReason string
	ObservedAt    time.Time
}

// HumanWorkItem is one pending human task or approval an [ExecutionTrace]
// reports outstanding against the plan. AssignedTo names a principal or
// role reference, never a raw identity document; this package treats it as
// PII-adjacent and gates it the same way it gates evidence (see
// [FieldHumanWork]).
type HumanWorkItem struct {
	NodeID      string
	Kind        string // "APPROVAL" | "TASK", mirroring workflow.StepApproval / workflow.StepTask.
	AssignedTo  string
	RequestedAt time.Time
}

// ExecutionTrace is the port [BuildWorkflowView] reads execution evidence
// through. *[SimulationReceipt] satisfies it today; a durable runtime's own
// execution log satisfies it once WF-RUN-019 ships, without this package or
// its callers changing.
type ExecutionTrace interface {
	// NodeEvidence returns the evidence recorded for one node, and whether
	// any was recorded at all. A trace that has nothing for a node returns
	// ok=false; [BuildWorkflowView] then reports that node's status as
	// [StatusUnknown] rather than inventing a PENDING guess.
	NodeEvidence(nodeID string) (NodeEvidence, bool)
	// PendingHumanWork returns every human work item the trace considers
	// still outstanding against the plan.
	PendingHumanWork() []HumanWorkItem
}

// SimulationReceipt is the minimal P1A [ExecutionTrace]: a node list plus
// per-node status/evidence, exactly the shape a SIMULATE-mode run can
// produce without a durable runtime behind it (ADMIN-002's brief: "in P1A
// the source is a simulation receipt shape you define minimally"). Its zero
// value is a valid, empty trace: every node reports [StatusUnknown] and no
// human work is pending.
type SimulationReceipt struct {
	WorkflowID string
	Version    uint32
	// Nodes maps node ID to the evidence recorded for it. A node absent
	// from this map was not reached by the simulated run.
	Nodes map[string]NodeEvidence
	// PendingWork lists the human work items the simulated run reports
	// outstanding.
	PendingWork []HumanWorkItem
}

var _ ExecutionTrace = SimulationReceipt{}

// NodeEvidence implements [ExecutionTrace].
func (r SimulationReceipt) NodeEvidence(nodeID string) (NodeEvidence, bool) {
	ev, ok := r.Nodes[nodeID]
	return ev, ok
}

// PendingHumanWork implements [ExecutionTrace].
func (r SimulationReceipt) PendingHumanWork() []HumanWorkItem {
	return r.PendingWork
}

// emptyTrace is used when BuildWorkflowView is called with a nil trace, so
// every node deterministically resolves to StatusUnknown rather than the
// caller needing to construct an empty SimulationReceipt itself.
type emptyTrace struct{}

func (emptyTrace) NodeEvidence(string) (NodeEvidence, bool) { return NodeEvidence{}, false }
func (emptyTrace) PendingHumanWork() []HumanWorkItem        { return nil }
