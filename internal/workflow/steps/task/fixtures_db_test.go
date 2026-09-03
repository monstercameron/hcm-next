package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// TestMain starts the one embedded PostgreSQL instance the DB-backed tests in
// this package share, per WF-STEP-004's instruction that DB-backed tests use
// pgtest.New(t). The server starts lazily on the first test that asks for one
// (see pgtest.RunMain), so the pure Conformance/Security/Mutation/Browser
// cases below never pay for it.
func TestMain(m *testing.M) { pgtest.RunMain(m) }

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func insertInstance(t *testing.T, db *pgtest.DB, tenant, instanceID uuid.UUID, nodeID string) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wf.promotion', 1,
			'`+zeroDigest+`', 'SIMULATE', 'RUNNING', 'sha256:input',
			ARRAY[$3], $4, $5)`,
		tenant, instanceID, nodeID, "corr-"+instanceID.String(), time.Now().UTC())
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

// inTenantTxErr is inTenantTx for a call whose own error the test wants to
// inspect. A failing fn rolls the transaction back, so a refused write leaves
// nothing behind.
func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
