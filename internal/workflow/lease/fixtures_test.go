package lease_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps. Nothing in
// internal/workflow/lease reads a wall clock, so a test that wants a time has
// to name it.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// holderA and holderB are two distinct workload identities. Both are
// scheme-qualified with a replica reference, which is what WF-RUN-002's
// REFACTOR clause requires of a lease owner.
var (
	holderA = lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:cell-local-1"}
	holderB = lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:cell-local-2"}
)

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

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, which is the only way a test observes the
// row level security migration 00026 declares.
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

// leaseFixture is one tenant, one app-role connection and one resource.
type leaseFixture struct {
	tenant   uuid.UUID
	conn     *pgxadapter.Conn
	resource lease.Resource
	manager  lease.Manager
}

func newLeaseFixture(t *testing.T, db *pgtest.DB, key string) leaseFixture {
	t.Helper()
	return leaseFixture{
		tenant:   insertTenant(t, db, key),
		conn:     appConn(t, db),
		resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: "instance:" + key},
	}
}

// acquire takes the lease in its own committed transaction and fails the test
// if it is refused.
func (f leaseFixture) acquire(t *testing.T, holder lease.Identity, now time.Time, ttl time.Duration) lease.Grant {
	t.Helper()
	grant, err := f.tryAcquire(holder, now, ttl)
	if err != nil {
		t.Fatalf("acquire %s: %v", holder, err)
	}
	return grant
}

func (f leaseFixture) tryAcquire(holder lease.Identity, now time.Time, ttl time.Duration) (lease.Grant, error) {
	var grant lease.Grant
	err := inTenantTxErr(f.conn, f.tenant, func(tx dbport.Tx) error {
		var acqErr error
		grant, acqErr = f.manager.Acquire(context.Background(), tx, lease.AcquireRequest{
			TenantID: f.tenant, Resource: f.resource, Holder: holder, Now: now, TTL: ttl,
		})
		return acqErr
	})
	return grant, err
}

// do runs one manager call in its own committed tenant transaction.
func (f leaseFixture) do(t *testing.T, fn func(tx dbport.Tx) error) {
	t.Helper()
	inTenantTx(t, f.conn, f.tenant, fn)
}

// try runs one manager call in its own tenant transaction and hands back the
// error, rolling back so a refused call leaves nothing behind.
func (f leaseFixture) try(fn func(tx dbport.Tx) error) error {
	return inTenantTxErr(f.conn, f.tenant, fn)
}
