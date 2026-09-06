package execute

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// TestResumeRefusesWhileTheInstanceIsPaused is WF-RUN-008's driver-facing
// half: a governed pause stops the driver, not only a direct
// [runtime.Advance] caller.
//
// The refusal is not implemented here. [runtime.Advance] holds the one pause
// gate, inside the same fenced transaction as the advancement it guards, and
// this driver surfaces its typed refusal unchanged -- which is exactly the
// property worth pinning: a second, driver-local pause check could drift out
// of agreement with the runtime's, and there is none.
func TestResumeRefusesWhileTheInstanceIsPaused(t *testing.T) {
	f := newWfrun028Fixture(t, "paused")

	var paused runtime.PauseReceipt
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var pauseErr error
		paused, pauseErr = runtime.RequestPause(context.Background(), tx, runtime.PauseRequest{
			TenantID: f.tenantID, InstanceID: f.instanceID,
			ExpectedInstanceVersion: f.instanceVersion,
			Plan:                    f.plan,
			Reason:                  "INCIDENT_REVIEW",
			RequestedBy:             "principal:operations-duty",
			RequestedAt:             f.at,
		})
		return pauseErr
	})
	if paused.Status != runtime.InstancePaused {
		t.Fatalf("pause at a parked safe point produced %s, want PAUSED", paused.Status)
	}

	req := f.resumeRequest()
	req.ExpectedInstanceVersion = paused.InstanceVersion
	_, err := f.driver(t).Resume(context.Background(), req)
	if runtime.CodeOf(err) != runtime.CodeInstancePaused {
		t.Fatalf("Resume of a paused instance: code = %q, want %q (%v)",
			runtime.CodeOf(err), runtime.CodeInstancePaused, err)
	}

	// And the refusal changed nothing: the work item is untouched and the
	// instance is still exactly where the pause left it.
	var inst runtime.Instance
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var loadErr error
		inst, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		return loadErr
	})
	if inst.RuntimeStatus != runtime.InstancePaused || inst.InstanceVersion != paused.InstanceVersion {
		t.Fatalf("after a refused resume the instance is %s at version %d, want PAUSED at %d",
			inst.RuntimeStatus, inst.InstanceVersion, paused.InstanceVersion)
	}
}
