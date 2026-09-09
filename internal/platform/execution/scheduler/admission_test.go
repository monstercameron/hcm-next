package scheduler

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestAdmissibleAdmitsOnlyAdvanceableStatuses is the admission rule as a
// table over every declared instance status: exactly CREATED, RUNNING and
// WAITING are publishable, and every governed intervention state -- PAUSED,
// PAUSE_REQUESTED, QUARANTINED, REPAIR_REQUIRED -- is not.
func TestAdmissibleAdmitsOnlyAdvanceableStatuses(t *testing.T) {
	want := map[runtime.InstanceStatus]bool{
		runtime.InstanceCreated:        true,
		runtime.InstanceRunning:        true,
		runtime.InstanceWaiting:        true,
		runtime.InstancePauseRequested: false,
		runtime.InstancePaused:         false,
		runtime.InstanceCancelling:     false,
		runtime.InstanceBlocked:        false,
		runtime.InstanceCompleted:      false,
		runtime.InstanceCancelled:      false,
		runtime.InstanceRepairRequired: false,
		runtime.InstanceQuarantined:    false,
		runtime.InstanceSuperseded:     false,
	}
	statuses := runtime.InstanceStatuses()
	if len(statuses) != len(want) {
		t.Fatalf("runtime declares %d instance statuses, this admission table covers %d; the table is stale",
			len(statuses), len(want))
	}
	for _, status := range statuses {
		expected, known := want[status]
		if !known {
			t.Fatalf("instance status %s has no admission decision", status)
		}
		if got := Admissible(string(status)); got != expected {
			t.Errorf("Admissible(%s) = %v, want %v", status, got, expected)
		}
	}
	if Admissible("NOT_A_STATUS") {
		t.Error("an unknown status is admissible")
	}
}

// TestAdmissibleStatusesCannotBeWidenedByACaller proves the accessor hands
// back a copy: an admission rule a caller could append to would not be a rule.
func TestAdmissibleStatusesCannotBeWidenedByACaller(t *testing.T) {
	got := AdmissibleStatuses()
	if len(got) != 3 {
		t.Fatalf("AdmissibleStatuses() = %v, want three statuses", got)
	}
	got[0] = string(runtime.InstancePaused)
	got = append(got, string(runtime.InstanceQuarantined))
	if Admissible(string(runtime.InstancePaused)) || Admissible(string(runtime.InstanceQuarantined)) {
		t.Fatalf("mutating the returned slice widened the admission rule to %v", AdmissibleStatuses())
	}
	if !Admissible(string(runtime.InstanceCreated)) {
		t.Fatal("mutating the returned slice narrowed the admission rule")
	}
}

// TestSelectionStatementsCarryTheirConcurrencyClauses pins the two properties
// the SQL, not Go, is responsible for: a total publication order and a
// non-blocking row claim.
func TestSelectionStatementsCarryTheirConcurrencyClauses(t *testing.T) {
	for name, stmt := range map[string]string{"eligible": selectEligible, "abandoned": selectAbandoned} {
		if !strings.Contains(stmt, "FOR UPDATE OF rw SKIP LOCKED") {
			t.Errorf("%s selection does not take a skippable row lock: %s", name, stmt)
		}
		if !strings.Contains(stmt, readyOrder) {
			t.Errorf("%s selection does not use the declared publication order", name)
		}
	}
	// The order must end in the row's own identity, or two rows sharing a
	// priority, an eligibility instant and an arrival instant would come back
	// in an order PostgreSQL is free to vary between replicas.
	if !strings.HasSuffix(readyOrder, "rw.ready_work_id ASC") {
		t.Fatalf("publication order %q does not end in a unique tiebreak", readyOrder)
	}
	if !strings.Contains(selectEligible, "wi.runtime_status = ANY ($4)") {
		t.Error("the eligible selection does not apply the admission rule in the database")
	}
	if strings.Contains(selectAbandoned, "workflow_instance") {
		t.Error("recovering abandoned work must not be gated on the instance's current status")
	}
}

// TestInstanceResourceLeasesTheInstance records why a claim leases the
// instance rather than the ready-work row: two units of work for one instance
// would otherwise be advanced concurrently and race on the instance version.
func TestInstanceResourceLeasesTheInstance(t *testing.T) {
	row := readyWorkFixture()
	res := instanceResource(row)
	if res.Kind != lease.ResourceWorkflowInstance {
		t.Fatalf("claim leases %q, want WORKFLOW_INSTANCE", res.Kind)
	}
	if res.ID != row.InstanceID.String() {
		t.Fatalf("claim leases %q, want the instance id %q", res.ID, row.InstanceID.String())
	}
}

// TestPausedAndQuarantinedInstancesAreNotPublished is the admission rule
// against real rows: a governed intervention on the instance takes its ready
// work out of circulation without touching the ready-work row at all, and puts
// it back the moment the intervention is lifted.
//
// It matters that the row itself is untouched. An admission rule that
// cancelled the work would make a pause destructive; this one only declines to
// publish, so the durable frontier survives the intervention exactly as it was.
func TestPausedAndQuarantinedInstancesAreNotPublished(t *testing.T) {
	ctx := context.Background()
	db, tenantID, cell := newWorld(t, "svc004-admission")
	clock := newStepClock(fixtureAt)

	instance := park(t, db, tenantID, cell, "svc004-admission-1", fixtureAt)

	// A publish-only replica fires the promise into a ready-work row and
	// claims nothing, which is this release's cmd/scheduler posture.
	publisher, _ := newReplica(t, db, tenantID, "replica:publisher", testQueue, nil, clock)
	clock.set(fixtureFireAt)
	published, err := publisher.Tick(ctx)
	if err != nil {
		t.Fatalf("publisher tick: %v", err)
	}
	if published.Fired != 1 || published.Claimed != 0 {
		t.Fatalf("publish-only tick fired %d and claimed %d; want 1 and 0", published.Fired, published.Claimed)
	}

	var readyID uuid.UUID
	if err := db.QueryRow(ctx, `
		SELECT ready_work_id FROM workflow_ready_work WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instance.instanceID).Scan(&readyID); err != nil {
		t.Fatalf("read the woken ready work: %v", err)
	}

	var claims int
	worker, _ := newReplica(t, db, tenantID, "replica:worker", testQueue,
		DispatcherFunc(func(context.Context, Work) (Disposition, error) {
			claims++
			return DispositionCompleted, nil
		}), clock)

	for _, status := range []string{
		string(runtime.InstancePaused),
		string(runtime.InstancePauseRequested),
		string(runtime.InstanceQuarantined),
		string(runtime.InstanceRepairRequired),
	} {
		setInstanceStatus(t, db, tenantID, instance.instanceID, status)
		result, err := worker.Tick(ctx)
		if err != nil {
			t.Fatalf("tick against a %s instance: %v", status, err)
		}
		if result.Selected != 0 || result.Claimed != 0 || claims != 0 {
			t.Fatalf("a %s instance had work published: selected=%d claimed=%d dispatched=%d",
				status, result.Selected, result.Claimed, claims)
		}
		state, version := readyStateOf(t, db, tenantID, readyID)
		if state != runtimestate.ReadyReady || version != 1 {
			t.Fatalf("a %s instance's ready work is %s at version %d; the admission rule must not touch the row",
				status, state, version)
		}
	}

	// Lifting the intervention makes exactly the same row publishable again.
	setInstanceStatus(t, db, tenantID, instance.instanceID, string(runtime.InstanceWaiting))
	resumed, err := worker.Tick(ctx)
	if err != nil {
		t.Fatalf("tick against a resumed instance: %v", err)
	}
	if resumed.Claimed != 1 || claims != 1 {
		t.Fatalf("a resumed instance claimed %d and dispatched %d, want 1 and 1", resumed.Claimed, claims)
	}
	if state, _ := readyStateOf(t, db, tenantID, readyID); state != runtimestate.ReadyDone {
		t.Fatalf("settled ready work is %s, want DONE", state)
	}
}
