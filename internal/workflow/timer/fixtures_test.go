package timer_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps. Nothing in
// internal/workflow/timer reads a wall clock, so a test that wants a time has
// to name it.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// The zone, calendar and dataset the wake requirements below are pinned
// against, matching internal/workflow/steps/wait's own fixtures.
var (
	testZone     = values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}
	testCalendar = values.CalendarRef{Ref: "us-federal", Version: "2026.1"}
	testDataset  = values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}
	// republished is the same zone and calendar after a dataset release. It
	// is what makes "a dataset revision must not silently change an execution
	// date" testable: the requirement digest moves with it.
	republished = values.DatasetVersions{TzdbVersion: "2026b", CalendarVersion: "2026.2"}
)

var timerHolder = lease.Identity{
	WorkloadRef: "workload:hcmnext-workflow-runtime",
	InstanceRef: "replica:timer-1",
}

// wakeRequirement builds a real wait.TimerRequirement for a fixed wake
// instant through internal/workflow/steps/wait's own pure computation, so the
// digest this package keys a timer on is the digest a WAIT step would
// actually produce.
func wakeRequirement(t *testing.T, nodeID string, fireAt time.Time, dataset values.DatasetVersions) wait.TimerRequirement {
	t.Helper()
	req, err := wait.ComputeTimerRequirement(wait.CompiledWaitNode{
		WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: nodeID,
		WakeInstant: values.NewInstant(fireAt),
		Zone:        testZone, Calendar: testCalendar, Policy: values.ReferenceUpdatePin,
	}, dataset)
	if err != nil {
		t.Fatalf("ComputeTimerRequirement(%s): %v", nodeID, err)
	}
	return req
}

// insertTenant registers one active tenant as the migration/admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTxErr(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	return inTxErr(conn, func(tx dbport.Tx) error {
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

// referencePlan compiles the promotion reference workflow, the one real
// compiled plan this repository has, so a persisted instance carries a real
// workflow id, version, plan digest and start node.
func referencePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatalf("compile the promotion reference: %v", err)
	}
	return setup.Plan
}

// timerFixture is one tenant, one app-role connection, one persisted workflow
// instance (workflow_timer has a foreign key to it) and one lease the fire
// path is fenced by.
type timerFixture struct {
	db       *pgtest.DB
	tenant   uuid.UUID
	conn     *pgxadapter.Conn
	instance uuid.UUID
	fence    lease.Fence
	resource lease.Resource
}

func newTimerFixture(t *testing.T, db *pgtest.DB, key string) timerFixture {
	t.Helper()
	ctx := context.Background()
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)
	plan := referencePlan(t)

	inst, err := runtime.NewInstance(tenant, uuid.New(), "cell-local", plan, workflow.ModeSimulate,
		"sha256:"+repeatHex("1"), "corr-"+key, fixedInstant)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, cerr := (runtime.Store{}).CreateInstance(ctx, tx, inst)
		return cerr
	})

	// The timer queue lease: one holder settles a tenant's due promises, and
	// every settle presents its fence.
	resource := lease.Resource{Kind: lease.ResourceQueue, ID: "queue:workflow-timer"}
	var grant lease.Grant
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var aerr error
		grant, aerr = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenant, Resource: resource, Holder: timerHolder,
			Now: fixedInstant, TTL: 720 * time.Hour,
		})
		return aerr
	})

	return timerFixture{
		db: db, tenant: tenant, conn: conn, instance: inst.InstanceID,
		fence: grant.Fence, resource: resource,
	}
}

func (f timerFixture) do(t *testing.T, fn func(tx dbport.Tx) error) {
	t.Helper()
	inTenantTx(t, f.conn, f.tenant, fn)
}

func (f timerFixture) try(fn func(tx dbport.Tx) error) error {
	return inTenantTxErr(f.conn, f.tenant, fn)
}

func repeatHex(digit string) string {
	out := ""
	for range 64 {
		out += digit
	}
	return out
}
