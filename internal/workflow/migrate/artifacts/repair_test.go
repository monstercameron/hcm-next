package artifacts

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

func TestRepairRequest_ValidateRefusesMalformedMarkers(t *testing.T) {
	t.Parallel()
	base := RepairRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(),
		Reason: CodeNotRelocatable, RecordedBy: "principal:operator", RecordedAt: fixedInstant,
	}
	if err := base.validate(); err != nil {
		t.Fatalf("a well-formed repair request was refused: %v", err)
	}
	cases := map[string]func(r *RepairRequest){
		"nil tenant":   func(r *RepairRequest) { r.TenantID = uuid.Nil },
		"nil instance": func(r *RepairRequest) { r.InstanceID = uuid.Nil },
		"no reason":    func(r *RepairRequest) { r.Reason = "" },
		"no recorder":  func(r *RepairRequest) { r.RecordedBy = "" },
		"no instant":   func(r *RepairRequest) { r.RecordedAt = time.Time{} },
	}
	for name, mutate := range cases {
		req := base
		mutate(&req)
		err := req.validate()
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if got := CodeOf(err); got != CodeInvalidRequest {
			t.Fatalf("%s refused with %q, want %q", name, got, CodeInvalidRequest)
		}
	}
}

// The path is whatever internal/workflow/runtime's own transition map allows,
// which is the point: a change to that machine moves this function with it
// rather than leaving a stale hard-coded route behind.
func TestRepairPath_FollowsTheRuntimeStateMachine(t *testing.T) {
	t.Parallel()
	for _, from := range runtime.InstanceStatuses() {
		path, ok := repairPath(from)
		if !ok {
			if from.Terminal() && from != runtime.InstanceRepairRequired {
				continue
			}
			t.Fatalf("%s has no path to REPAIR_REQUIRED but is not terminal", from)
		}
		if len(path) == 0 || len(path) > 2 {
			t.Fatalf("%s produced a %d-step path %v", from, len(path), path)
		}
		if path[len(path)-1] != runtime.InstanceRepairRequired {
			t.Fatalf("%s produced a path ending at %s", from, path[len(path)-1])
		}
		current := from
		for _, step := range path {
			if !runtime.LegalInstanceTransition(current, step) {
				t.Fatalf("from %s the path %v takes the illegal step %s -> %s", from, path, current, step)
			}
			current = step
		}
	}
}

// A PAUSED instance is the exact case WF-RUN-018 leaves behind, and the state
// machine gives it no direct edge to REPAIR_REQUIRED -- so the path must go
// through one recorded intermediate rather than skipping it.
func TestRepairPath_PausedInstanceRoutesThroughOneIntermediate(t *testing.T) {
	t.Parallel()
	path, ok := repairPath(runtime.InstancePaused)
	if !ok {
		t.Fatal("a PAUSED instance has no path to REPAIR_REQUIRED")
	}
	if len(path) != 2 {
		t.Fatalf("PAUSED path = %v, want two steps", path)
	}
	if runtime.LegalInstanceTransition(runtime.InstancePaused, runtime.InstanceRepairRequired) {
		t.Fatal("the runtime state machine now allows PAUSED -> REPAIR_REQUIRED directly; this test's premise is stale")
	}
}

func TestRepairPath_AlreadyRepairRequiredIsIdempotent(t *testing.T) {
	t.Parallel()
	path, ok := repairPath(runtime.InstanceRepairRequired)
	if !ok || len(path) != 1 || path[0] != runtime.InstanceRepairRequired {
		t.Fatalf("repairPath(REPAIR_REQUIRED) = %v, %v", path, ok)
	}
}

func TestRepairPath_TerminalInstancesHaveNoPath(t *testing.T) {
	t.Parallel()
	for _, terminal := range []runtime.InstanceStatus{
		runtime.InstanceCompleted, runtime.InstanceCancelled,
		runtime.InstanceQuarantined, runtime.InstanceSuperseded,
	} {
		if _, ok := repairPath(terminal); ok {
			t.Fatalf("%s reported a path to REPAIR_REQUIRED; history is not repairable in place", terminal)
		}
	}
}
