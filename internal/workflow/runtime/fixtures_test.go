package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture stamps. This package never reads a
// wall clock, so a test that wants a time has to say which one.
var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// insertTenant registers one active tenant as the migration/admin role (the
// pgtest connection is a superuser and is therefore never itself subject to
// row level security) and returns its identifier.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// referencePlan compiles the promotion reference workflow, which is the one
// real compiled plan this repository has. Using it rather than a hand-built
// stub means the stored workflow id, version, plan digest and start node are
// the same values a real instance would carry.
func referencePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatalf("compile the promotion reference: %v", err)
	}
	return setup.Plan
}

// newInstance builds a CREATED instance for tenant against the reference plan.
func newInstance(t *testing.T, tenant uuid.UUID, plan *workflow.CompiledWorkflow) runtime.Instance {
	t.Helper()
	inst, err := runtime.NewInstance(
		tenant, uuid.New(), "cell-local", plan, workflow.ModeSimulate,
		"sha256:input-snapshot", "corr-"+tenant.String(), fixedInstant)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	return inst
}

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, which is the only way a test observes the
// row level security policies migration 00016 declares: the pgtest URL
// authenticates as a superuser, and a superuser bypasses every policy.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// inTxErr is inTx for a call whose own error the test wants to inspect. A
// failing fn rolls the transaction back, so a refused write leaves nothing
// behind.
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

// inTenantTx is inTx with the tenant scope set as its first statement, the way
// internal/data/tenancy documents. Every statement afterwards is confined to
// that tenant's rows for the lifetime of the transaction only.
func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	return inTxErr(conn, func(tx dbport.Tx) error {
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

func timePtr(t time.Time) *time.Time { return &t }
