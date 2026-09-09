package runtime

import (
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// nodeExecutionNamespace is the UUIDv5 namespace node execution identities are
// derived under. It is itself a fixed, checked-in UUID so the derivation is
// reproducible across processes and machines.
var nodeExecutionNamespace = uuid.MustParse("6f3f4a2e-7f2f-5e3b-9a1d-3f0a5c9d8e11")

// NodeExecutionID derives the stable identity of one node execution attempt
// from the tuple that defines it.
//
// Derivation rather than allocation is what makes "stable identities" in
// WF-RUN-001's GREEN clause checkable: a driver that crashed between deciding
// to record an attempt and recording it recomputes the same id on restart, so
// the retry collides on the primary key instead of minting a second identity
// for one execution.
func NodeExecutionID(tenantID, instanceID uuid.UUID, nodeID string, attempt int) uuid.UUID {
	name := tenantID.String() + "\x00" + instanceID.String() + "\x00" + nodeID + "\x00" + strconv.Itoa(attempt)
	return uuid.NewSHA1(nodeExecutionNamespace, []byte(name))
}

// GovernanceRefs are the typed references one node execution binds. They are
// the spine the execution inspector (WF-RUN-019) traverses, which is why they
// are named columns rather than an opaque blob: an inspector that had to parse
// JSON to find the proposal a node produced could silently start omitting it.
//
// Every field is a reference to something stored and authorized elsewhere.
// None of them carries content.
type GovernanceRefs struct {
	CapabilityExecutionID   string
	AuthorizationDecisionID string
	DecisionID              string
	HumanTaskID             string
	AgentExecutionID        string
	ProposalRef             string
	BaselineRef             string
	PolicyRef               string
	RepairRef               string
	EffectRefs              []string
	// RetryPolicyRef names the retry policy the compiled plan declared for
	// this node. It is a reference to a declared policy, never a scheduled
	// retry: nothing in this phase reads it to decide when anything runs.
	RetryPolicyRef string
}

// NodeExecution is one durable attempt at one node of one instance.
//
// The spec's entity additionally carries execution_lease_id and retry_at. Both
// are deliberately absent: WF-RUN-000 gates leases, fencing and timers behind
// the P1B re-evaluation, and a column only a scheduler could fill would be a
// scheduler nobody decided to build.
type NodeExecution struct {
	TenantID        uuid.UUID
	NodeExecutionID uuid.UUID
	InstanceID      uuid.UUID
	NodeID          string
	Attempt         int
	StepType        workflow.StepType
	Status          NodeStatus

	InputSnapshotRef  string
	OutputArtifactRef string

	Refs GovernanceRefs

	ErrorClass string
	TraceID    string

	StartedAt   *time.Time
	CompletedAt *time.Time
	RecordedAt  time.Time
}

// NewNodeExecution builds a node execution with its derived identity, ready
// for [Store.RecordNodeExecution].
func NewNodeExecution(
	tenantID, instanceID uuid.UUID,
	nodeID string,
	attempt int,
	stepType workflow.StepType,
	status NodeStatus,
) NodeExecution {
	return NodeExecution{
		TenantID:        tenantID,
		NodeExecutionID: NodeExecutionID(tenantID, instanceID, nodeID, attempt),
		InstanceID:      instanceID,
		NodeID:          nodeID,
		Attempt:         attempt,
		StepType:        stepType,
		Status:          status,
	}
}

// Validate reports whether the node execution is storable on its own terms.
func (n NodeExecution) Validate() error {
	instance := n.InstanceID.String()
	switch {
	case n.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, n.NodeID, "tenant id must not be the nil UUID")
	case n.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, n.NodeID, "instance id must not be the nil UUID")
	case n.NodeID == "":
		return refuse(CodeInvalidRecord, instance, n.NodeID, "node id is required")
	case n.Attempt < 1:
		return refuse(CodeInvalidRecord, instance, n.NodeID, "attempt must be at least 1")
	case n.StepType == "":
		return refuse(CodeInvalidRecord, instance, n.NodeID, "step type is required")
	case !n.Status.Valid():
		return refuse(CodeInvalidRecord, instance, n.NodeID, "node status %q is not declared", string(n.Status))
	case n.CompletedAt != nil && !n.Status.Finished():
		return refuse(CodeInvalidRecord, instance, n.NodeID,
			"completed_at is set but status %s has not finished the attempt", string(n.Status))
	}
	want := NodeExecutionID(n.TenantID, n.InstanceID, n.NodeID, n.Attempt)
	if n.NodeExecutionID != want {
		return refuse(CodeInvalidRecord, instance, n.NodeID,
			"node execution id %s is not the derived identity %s for attempt %d",
			n.NodeExecutionID, want, n.Attempt)
	}
	return nil
}

// NodeTransition is one recorded status change of an already-stored node
// execution, under the same instance-version check every runtime write takes.
type NodeTransition struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Attempt    int
	// ExpectedInstanceVersion is the instance_version the caller read.
	ExpectedInstanceVersion int64

	Status NodeStatus

	OutputArtifactRef string
	Refs              GovernanceRefs
	ErrorClass        string
	TraceID           string
	CompletedAt       *time.Time
}

// Validate reports whether the transition is well formed before any statement
// runs.
func (t NodeTransition) Validate() error {
	instance := t.InstanceID.String()
	switch {
	case t.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, t.NodeID, "tenant id must not be the nil UUID")
	case t.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, instance, t.NodeID, "instance id must not be the nil UUID")
	case t.NodeID == "":
		return refuse(CodeInvalidRecord, instance, t.NodeID, "node id is required")
	case t.Attempt < 1:
		return refuse(CodeInvalidRecord, instance, t.NodeID, "attempt must be at least 1")
	case t.ExpectedInstanceVersion < 1:
		return refuse(CodeInvalidRecord, instance, t.NodeID, "expected instance version must be at least 1")
	case !t.Status.Valid():
		return refuse(CodeInvalidRecord, instance, t.NodeID, "node status %q is not declared", string(t.Status))
	case t.CompletedAt != nil && !t.Status.Finished():
		return refuse(CodeInvalidRecord, instance, t.NodeID,
			"completed_at is set but status %s has not finished the attempt", string(t.Status))
	}
	return nil
}
