package runtime

import (
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// Dimensions is the five-dimension intent lifecycle state an instance carries
// beside its own runtime status, spelled as canonical state ids.
//
// It is a separate struct from lifecycle.Dimensions so that the stored JSON
// shape is this package's own and does not move when another package changes a
// field tag. There is no sixth field and no collapsed status: an instance may
// be COMPLETED with ConsistencyState DEGRADED, and flattening that would be
// the exact lie planning/specs/workflow-runtime.md forbids.
type Dimensions struct {
	RequestState     string `json:"request_state"`
	ExecutionState   string `json:"execution_state"`
	BusinessState    string `json:"business_state"`
	ConsistencyState string `json:"consistency_state"`
	ObligationState  string `json:"obligation_state"`
}

// DimensionsOf projects the intent kernel's own dimensions into the stored
// shape.
func DimensionsOf(d lifecycle.Dimensions) Dimensions {
	return Dimensions{
		RequestState:     string(d.State(lifecycle.DimensionRequest)),
		ExecutionState:   string(d.State(lifecycle.DimensionExecution)),
		BusinessState:    string(d.State(lifecycle.DimensionBusiness)),
		ConsistencyState: string(d.State(lifecycle.DimensionConsistency)),
		ObligationState:  string(d.State(lifecycle.DimensionObligation)),
	}
}

// Empty reports whether no dimension has been recorded yet, which is the
// honest state of an instance that has not reached a terminal.
func (d Dimensions) Empty() bool { return d == Dimensions{} }

// Instance is one durable workflow instance: the identity of what is running,
// where it is, and which version a writer must hold to change it.
//
// Every reference field is a reference. InputRef is the digest of the input
// snapshot, not the snapshot; EffectiveContextRef, LastCheckpointRef and
// BusinessSubjectRefs name artifacts stored under their own authorization.
// The runtime store answers current execution state; the ledger answers what
// the business did.
type Instance struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	CellID     string

	WorkflowID       string
	WorkflowVersion  uint32
	CompiledPlanHash string

	BusinessSubjectRefs   []string
	BusinessTransactionID *uuid.UUID

	ExecutionMode        workflow.ExecutionMode
	RuntimeStatus        InstanceStatus
	CompletionDimensions Dimensions

	InputRef             string
	VariableRevisionHead int64
	// CurrentNodeIDs is the execution frontier: every node the instance is
	// currently at. It is a set, not a single id, because a parallel region
	// has more than one current node, and losing it is the first RED case
	// WF-RUN-001 names.
	CurrentNodeIDs []string

	EffectiveContextRef string
	LastCheckpointRef   string
	// InstanceVersion is the optimistic concurrency token. A writer submits
	// the version it read; a writer whose version has been overtaken is
	// refused with CodeStaleInstance and mutates nothing.
	InstanceVersion int64

	CorrelationID string
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
}

// NewInstance builds a CREATED instance ready for [Store.CreateInstance],
// stamping the identity fields a caller should not have to assemble by hand.
//
// The frontier starts at the compiled plan's start node: an instance that
// existed with an empty frontier would be one nobody could resume after a
// restart.
func NewInstance(
	tenantID uuid.UUID,
	instanceID uuid.UUID,
	cellID string,
	plan *workflow.CompiledWorkflow,
	mode workflow.ExecutionMode,
	inputRef string,
	correlationID string,
	createdAt time.Time,
) (Instance, error) {
	if plan == nil {
		return Instance{}, refuse(CodeInvalidRecord, instanceID.String(), "", "no compiled plan")
	}
	if createdAt.IsZero() {
		return Instance{}, refuse(CodeInvalidRecord, instanceID.String(), "",
			"created_at must be supplied; this package never reads a wall clock")
	}
	inst := Instance{
		TenantID:             tenantID,
		InstanceID:           instanceID,
		CellID:               cellID,
		WorkflowID:           plan.WorkflowID,
		WorkflowVersion:      plan.Version,
		CompiledPlanHash:     plan.Digest(),
		ExecutionMode:        mode,
		RuntimeStatus:        InstanceCreated,
		InputRef:             inputRef,
		VariableRevisionHead: 0,
		CurrentNodeIDs:       []string{plan.StartNodeID},
		InstanceVersion:      1,
		CorrelationID:        correlationID,
		CreatedAt:            createdAt.UTC(),
	}
	if err := inst.Validate(); err != nil {
		return Instance{}, err
	}
	return inst, nil
}

// Validate reports whether the instance is storable on its own terms. It is
// deliberately strict about the fields a restart needs: an instance missing
// its plan hash, its input reference or its frontier is one no driver could
// pick up again, which is precisely WF-RUN-001's RED case.
func (i Instance) Validate() error {
	id := i.InstanceID.String()
	switch {
	case i.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "tenant id must not be the nil UUID")
	case i.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "instance id must not be the nil UUID")
	case i.CellID == "":
		return refuse(CodeInvalidRecord, id, "", "cell id is required")
	case i.WorkflowID == "":
		return refuse(CodeInvalidRecord, id, "", "workflow id is required")
	case i.WorkflowVersion == 0:
		return refuse(CodeInvalidRecord, id, "", "workflow version must be at least 1")
	case i.CompiledPlanHash == "":
		return refuse(CodeInvalidRecord, id, "", "compiled plan hash is required; an instance must pin what it runs")
	case i.InputRef == "":
		return refuse(CodeInvalidRecord, id, "", "input ref is required")
	case i.CorrelationID == "":
		return refuse(CodeInvalidRecord, id, "", "correlation id is required")
	case !i.RuntimeStatus.Valid():
		return refuse(CodeInvalidRecord, id, "", "runtime status %q is not declared", string(i.RuntimeStatus))
	case !modeValid(i.ExecutionMode):
		return refuse(CodeInvalidRecord, id, "", "execution mode %q is not declared", string(i.ExecutionMode))
	case i.InstanceVersion < 1:
		return refuse(CodeInvalidRecord, id, "", "instance version must be at least 1")
	case i.VariableRevisionHead < 0:
		return refuse(CodeInvalidRecord, id, "", "variable revision head must not be negative")
	case !i.RuntimeStatus.Terminal() && len(i.CurrentNodeIDs) == 0:
		return refuse(CodeInvalidRecord, id, "",
			"a %s instance must carry a frontier; an empty one could never be resumed", string(i.RuntimeStatus))
	case i.CompletedAt != nil && !i.RuntimeStatus.Terminal():
		return refuse(CodeInvalidRecord, id, "",
			"completed_at is set but runtime status %s does not end the instance", string(i.RuntimeStatus))
	}
	for _, node := range i.CurrentNodeIDs {
		if node == "" {
			return refuse(CodeInvalidRecord, id, "", "frontier carries an empty node id")
		}
	}
	return nil
}

// Frontier returns a copy of the current node ids, so a caller cannot mutate
// the record it was handed.
func (i Instance) Frontier() []string {
	return append([]string(nil), i.CurrentNodeIDs...)
}

// InstanceTransition is one recorded change to an instance. It carries the
// version the writer holds; the write is applied only if the stored version
// still matches, and every field below is written together with the version
// bump so a restart never sees half a transition.
type InstanceTransition struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// ExpectedVersion is the instance_version the caller read. A mismatch is
	// CodeStaleInstance and changes nothing.
	ExpectedVersion int64

	Status               InstanceStatus
	CurrentNodeIDs       []string
	VariableRevisionHead int64
	EffectiveContextRef  string
	LastCheckpointRef    string
	CompletionDimensions Dimensions

	StartedAt   *time.Time
	CompletedAt *time.Time
}

// Validate reports whether the transition is well formed before any database
// statement runs.
func (t InstanceTransition) Validate() error {
	id := t.InstanceID.String()
	switch {
	case t.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "tenant id must not be the nil UUID")
	case t.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "instance id must not be the nil UUID")
	case t.ExpectedVersion < 1:
		return refuse(CodeInvalidRecord, id, "", "expected instance version must be at least 1")
	case !t.Status.Valid():
		return refuse(CodeInvalidRecord, id, "", "runtime status %q is not declared", string(t.Status))
	case t.VariableRevisionHead < 0:
		return refuse(CodeInvalidRecord, id, "", "variable revision head must not be negative")
	case !t.Status.Terminal() && len(t.CurrentNodeIDs) == 0:
		return refuse(CodeInvalidRecord, id, "",
			"a %s instance must carry a frontier", string(t.Status))
	case t.CompletedAt != nil && !t.Status.Terminal():
		return refuse(CodeInvalidRecord, id, "",
			"completed_at is set but runtime status %s does not end the instance", string(t.Status))
	}
	return nil
}

func modeValid(m workflow.ExecutionMode) bool {
	switch m {
	case workflow.ModeSimulate, workflow.ModeExecute, workflow.ModeReplay,
		workflow.ModeRepair, workflow.ModeShadow:
		return true
	default:
		return false
	}
}
