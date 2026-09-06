package lease

import (
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/data/runtimestate"
)

// The lease resource kinds, re-exported from the durable store so a caller of
// this package does not have to name internal/data/runtimestate to say what it
// is leasing.
const (
	ResourceWorkflowInstance = runtimestate.LeaseWorkflowInstance
	ResourceNodeExecution    = runtimestate.LeaseNodeExecution
	ResourceWorkItem         = runtimestate.LeaseWorkItem
	ResourceQueue            = runtimestate.LeaseQueue
)

// Resource names one leasable runtime resource: its declared kind and the
// identity of the particular instance, node execution, work item or queue.
type Resource struct {
	Kind string
	ID   string
}

// String renders the resource for a message or a log line.
func (r Resource) String() string { return r.Kind + " " + r.ID }

func (r Resource) validate() error {
	switch r.Kind {
	case ResourceWorkflowInstance, ResourceNodeExecution, ResourceWorkItem, ResourceQueue:
	default:
		return invalid(r, "", "resource kind %q is not one of WORKFLOW_INSTANCE, NODE_EXECUTION, WORK_ITEM, QUEUE", r.Kind)
	}
	if strings.TrimSpace(r.ID) == "" || r.ID != strings.TrimSpace(r.ID) {
		return invalid(r, "", "resource id must be non-blank and unpadded")
	}
	return nil
}

// holderSeparator joins the two halves of a holder id. It is '#' rather than
// anything a workload or replica reference is likely to contain, so
// [ParseHolder] can split a stored holder_id back into its parts without
// ambiguity.
const holderSeparator = "#"

// workloadScheme is the prefix a workload reference must carry.
const workloadScheme = "workload:"

// Identity is who holds a lease.
//
// WF-RUN-002's REFACTOR clause is the reason this is two fields rather than
// one string: "lease owner is workload identity, not process hostname alone".
// WorkloadRef names the deployed workload the code belongs to and must be
// scheme-qualified ("workload:hcmnext-workflow-runtime"); InstanceRef names
// the particular replica or process running it. A bare hostname is refused,
// because a hostname alone cannot tell an operator which code was holding the
// resource, and cannot survive a rescheduled replica keeping the same name.
type Identity struct {
	WorkloadRef string
	InstanceRef string
}

// HolderID is the canonical holder_id this identity is stored as.
func (i Identity) HolderID() string {
	return i.WorkloadRef + holderSeparator + i.InstanceRef
}

// String renders the holder id.
func (i Identity) String() string { return i.HolderID() }

func (i Identity) validate(res Resource) error {
	switch {
	case strings.TrimSpace(i.WorkloadRef) == "":
		return invalid(res, "", "a lease holder names the workload it belongs to, not a process alone")
	case !strings.HasPrefix(i.WorkloadRef, workloadScheme):
		return invalid(res, i.WorkloadRef,
			"workload reference %q is not scheme-qualified; a bare hostname is not a workload identity", i.WorkloadRef)
	case strings.TrimSpace(strings.TrimPrefix(i.WorkloadRef, workloadScheme)) == "":
		return invalid(res, i.WorkloadRef, "workload reference carries no workload name after %q", workloadScheme)
	case strings.TrimSpace(i.InstanceRef) == "":
		return invalid(res, i.WorkloadRef, "a lease holder names the replica or process instance running the workload")
	case strings.Contains(i.WorkloadRef, holderSeparator) || strings.Contains(i.InstanceRef, holderSeparator):
		return invalid(res, i.WorkloadRef, "neither half of a holder identity may contain %q", holderSeparator)
	case i.WorkloadRef != strings.TrimSpace(i.WorkloadRef) || i.InstanceRef != strings.TrimSpace(i.InstanceRef):
		return invalid(res, i.WorkloadRef, "holder identity halves must be unpadded")
	}
	return nil
}

// ParseHolder splits a stored holder_id back into its two halves. It is the
// exact inverse of [Identity.HolderID].
func ParseHolder(holderID string) (Identity, error) {
	workload, instance, ok := strings.Cut(holderID, holderSeparator)
	if !ok {
		return Identity{}, fmt.Errorf("%w: holder id %q carries no %q separator", ErrInvalid, holderID, holderSeparator)
	}
	id := Identity{WorkloadRef: workload, InstanceRef: instance}
	if err := id.validate(Resource{Kind: ResourceWorkflowInstance, ID: "unknown"}); err != nil {
		return Identity{}, err
	}
	return id, nil
}
