package reconcile_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/effectgraph"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps. Nothing in
// internal/operations/reconcile reads a wall clock, so a test that wants a
// time has to name it, matching internal/workflow/timer's own convention.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

var reconcileHolder = lease.Identity{
	WorkloadRef: "workload:hcmnext-operations-reconcile",
	InstanceRef: "replica:reconcile-1",
}

// mandatoryEffect builds a real, effectgraph.Compile-legal EffectNode whose
// observation contract is required, so the job this package triggers for it
// watches a promise the effect graph itself would accept, not a fixture
// invented independently of EFFECT-001's rules.
func mandatoryEffect(idempotencyKey, repairPolicy string) effectgraph.EffectNode {
	return effectgraph.EffectNode{
		ID: "node.provision_seat", ProposalRef: "proposal/1", TransactionRef: "tx/1",
		CapabilityRef: "payroll.write/v1", ResourceKey: "worker/1", OrderingKey: "worker/1",
		Ordering: effectgraph.Strict, IdempotencyKey: idempotencyKey,
		DispatchCondition: "approved", Deadline: "2026-12-01T00:00:00Z",
		FailurePolicy: "RETRY_THEN_QUARANTINE", CompensationPolicy: "NONE",
		RepairPolicy: repairPolicy, TerminalContribution: "OBSERVED",
		Observation: effectgraph.ObservationContract{Required: true, Profile: "provider-state/v1"},
	}
}

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

// reconcileFixture is one tenant, one app-role connection and one live lease
// on the reconciliation queue resource this package's writes are fenced by.
type reconcileFixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	conn   *pgxadapter.Conn
	fence  lease.Fence
}

func newReconcileFixture(t *testing.T, db *pgtest.DB, key string) reconcileFixture {
	t.Helper()
	ctx := context.Background()
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)

	resource := lease.Resource{Kind: lease.ResourceQueue, ID: "queue:reconciliation"}
	var grant lease.Grant
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		grant, err = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: tenant, Resource: resource, Holder: reconcileHolder,
			Now: fixedInstant, TTL: 720 * time.Hour,
		})
		return err
	})
	return reconcileFixture{db: db, tenant: tenant, conn: conn, fence: grant.Fence}
}

func (f reconcileFixture) do(t *testing.T, fn func(tx dbport.Tx) error) {
	t.Helper()
	inTenantTx(t, f.conn, f.tenant, fn)
}

func (f reconcileFixture) try(fn func(tx dbport.Tx) error) error {
	return inTenantTxErr(f.conn, f.tenant, fn)
}
