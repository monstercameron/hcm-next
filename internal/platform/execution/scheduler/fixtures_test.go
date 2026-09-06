package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/runtimestate"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/engines/schedule"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// testQueue is the queue resource every fixture replica leases.
const testQueue = "queue:workflow-runtime"

// fixedFireAt is the wake instant the promotion fixture's WAIT node names, and
// the instant every clock in these tests is driven from. Nothing here sleeps.
var (
	fixtureAt     = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fixtureFireAt = fixtureAt.Add(48 * time.Hour)
)

// readyWorkFixture is a plausible ready-work row for the tests that need one
// without a database behind it.
func readyWorkFixture() runtimestate.ReadyWork {
	return runtimestate.ReadyWork{
		TenantID: uuid.New(), ReadyWorkID: uuid.New(), InstanceID: uuid.New(),
		NodeID: "wait_effective_date", Attempt: 1,
		State: runtimestate.ReadyReady, Priority: 100,
		EligibleAt: fixtureAt, Version: 1, EnqueuedAt: fixtureAt,
	}
}

// claimFixture is one (tenant, queue, holder) claim for replica.
func claimFixture(tenantID uuid.UUID, replica string) lease.AcquireRequest {
	return lease.AcquireRequest{
		TenantID: tenantID,
		Resource: lease.Resource{Kind: lease.ResourceQueue, ID: testQueue},
		Holder:   lease.Identity{WorkloadRef: "workload:hcmnext-scheduler", InstanceRef: replica},
	}
}

// catchUpOnce is the misfire policy every fixture replica declares: an overdue
// promise fires once, at the instant it was noticed, within an hour's grace.
func catchUpOnce() schedule.MisfireConfig {
	return schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour}
}

// recordingLogger keeps every line a scheduler emitted so a test can assert on
// what it reported without a real logging backend.
type recordingLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *recordingLogger) Info(msg string, _ ...any)  { l.record(msg) }
func (l *recordingLogger) Error(msg string, _ ...any) { l.record(msg) }

func (l *recordingLogger) record(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, msg)
}

func (l *recordingLogger) count(msg string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if line == msg {
			n++
		}
	}
	return n
}

// failingBeginner is a Beginner that never opens a transaction, for the tests
// that assert a tick survives an unreachable database.
type failingBeginner struct{ err error }

func (b failingBeginner) Begin(context.Context) (dbport.Tx, error) { return nil, b.err }

// appConn opens an independent connection on the test's schema, running as the
// least-privilege application role, which is what every row-level-security
// policy in migration 00026 is written against. Two replicas need two of
// these: they must hold genuinely separate sessions.
func appConn(t *testing.T, db *pgtest.DB) dbport.Beginner {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// insertTenant registers one ACTIVE tenant.
func insertTenant(t *testing.T, db *pgtest.DB, key string, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`,
		id, key, "tenant "+key, at)
	return id
}

// inTenantTx runs fn in one committed transaction scoped to tenantID.
func inTenantTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// readyStateOf reads one ready-work row's durable state and version.
func readyStateOf(t *testing.T, db *pgtest.DB, tenantID, readyWorkID uuid.UUID) (string, int64) {
	t.Helper()
	var (
		state   string
		version int64
	)
	if err := db.QueryRow(context.Background(), `
		SELECT ready_state, ready_version FROM workflow_ready_work
		WHERE tenant_id = $1 AND ready_work_id = $2`, tenantID, readyWorkID).Scan(&state, &version); err != nil {
		t.Fatalf("read ready work %s: %v", readyWorkID, err)
	}
	return state, version
}

// setInstanceStatus forces one instance's runtime status, which is how the
// admission tests put an instance into a governed intervention state without
// driving a whole pause protocol through internal/workflow/runtime.
func setInstanceStatus(t *testing.T, db *pgtest.DB, tenantID, instanceID uuid.UUID, status string) {
	t.Helper()
	db.Exec(t, `
		UPDATE workflow_instance SET runtime_status = $3
		WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instanceID, status)
}
