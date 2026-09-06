package runtime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// WF-RUN-002's advancement-shaped half: runtime.AdvanceFenced.
//
// The claim under test is an ordering claim, not merely a refusal claim --
// "expired/stale worker ... completes a node, writes state or dispatches an
// effect" is RED, so a refused fenced advancement must not have read the
// instance, let alone written it. recordingExecutor is what turns that into
// an assertion: it remembers every statement AdvanceFenced issued, and the
// stale-fence case asserts that none of them touched a workflow table.

// recordingExecutor wraps a real transaction and remembers the SQL that went
// through it.
type recordingExecutor struct {
	tx dbport.Tx

	mu   sync.Mutex
	sqls []string
}

func (r *recordingExecutor) record(sql string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sqls = append(r.sqls, sql)
}

func (r *recordingExecutor) statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sqls...)
}

func (r *recordingExecutor) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	r.record(sql)
	return r.tx.Exec(ctx, sql, args...)
}

func (r *recordingExecutor) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	r.record(sql)
	return r.tx.Query(ctx, sql, args...)
}

func (r *recordingExecutor) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	r.record(sql)
	return r.tx.QueryRow(ctx, sql, args...)
}

var _ runtime.Executor = (*recordingExecutor)(nil)

// touchedWorkflowState reports whether any recorded statement read or wrote
// the runtime's own tables. workflow_lease is deliberately not in the list:
// verifying the fence is exactly what a fenced advancement is supposed to do
// before anything else.
func touchedWorkflowState(sqls []string) string {
	for _, sql := range sqls {
		for _, table := range []string{
			"workflow_instance", "workflow_node_execution", "workflow_continuation",
			"workflow_advancement_receipt",
		} {
			if strings.Contains(sql, table) {
				return table
			}
		}
	}
	return ""
}

// refusingVerifier refuses every fence without touching the database at all.
type refusingVerifier struct{ err error }

func (v refusingVerifier) VerifyFence(context.Context, runtime.Executor, uuid.UUID, runtime.Fence) error {
	return v.err
}

var (
	fencedHolderA = lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:a"}
	fencedHolderB = lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:b"}
)

func fenceOf(grant lease.Grant, at time.Time) runtime.Fence {
	return runtime.Fence{
		ResourceKind: grant.Fence.Resource.Kind,
		ResourceID:   grant.Fence.Resource.ID,
		LeaseID:      grant.Fence.LeaseID,
		HolderID:     grant.Fence.Holder.HolderID(),
		Token:        grant.Fence.Token,
		At:           at,
	}
}

func TestAdvanceFencedAdvancesUnderALiveFence(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "fenced-live")
	pf := newPromotionFixture(t, values.TenantId("fenced-live-tenant"), "intent:fenced-live")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-fenced-live"))
	resource := lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: start.InstanceID.String()}

	var manager lease.Manager
	var grant lease.Grant
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		grant, err = manager.Acquire(context.Background(), tx, lease.AcquireRequest{
			TenantID: tenantID, Resource: resource, Holder: fencedHolderA,
			Now: fixedInstant, TTL: time.Minute,
		})
		return err
	})

	sink := runtime.NewMemorySink()
	var receipt runtime.AdvanceReceipt
	var sqls []string
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		rec := &recordingExecutor{tx: tx}
		var advErr error
		receipt, advErr = runtime.AdvanceFenced(context.Background(), rec, runtime.FencedAdvanceRequest{
			Fence:    fenceOf(grant, fixedInstant.Add(time.Second)),
			Verifier: lease.Fenced{Manager: manager},
			Request: runtime.AdvanceRequest{
				TenantID: tenantID, InstanceID: start.InstanceID,
				ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1, Plan: pf.Plan,
				Outcome: frontier.NodeOutcome{
					NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded,
					OutputDigest: "sha256:snapshot",
				},
				RecordedAt: fixedInstant, Sink: sink,
			},
		})
		sqls = rec.statements()
		return advErr
	})
	if err != nil {
		t.Fatalf("AdvanceFenced under a live fence: %v", err)
	}
	if receipt.NewInstanceVersion <= start.InstanceVersion {
		t.Fatalf("instance version did not move: %d -> %d", start.InstanceVersion, receipt.NewInstanceVersion)
	}
	// The fence was verified, and the advancement then did its work.
	if !containsSQL(sqls, "workflow_lease") {
		t.Fatalf("a fenced advancement never read the lease: %v", sqls)
	}
	if touchedWorkflowState(sqls) == "" {
		t.Fatalf("an accepted fenced advancement never touched the runtime tables: %v", sqls)
	}
}

func TestAdvanceFencedRefusesAStaleFenceBeforeAnyRead(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "fenced-stale")
	pf := newPromotionFixture(t, values.TenantId("fenced-stale-tenant"), "intent:fenced-stale")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-fenced-stale"))
	resource := lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: start.InstanceID.String()}

	var manager lease.Manager
	var first lease.Grant
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		first, err = manager.Acquire(context.Background(), tx, lease.AcquireRequest{
			TenantID: tenantID, Resource: resource, Holder: fencedHolderA,
			Now: fixedInstant, TTL: 30 * time.Second,
		})
		return err
	})
	// A second workload takes the lapsed lease. The first holder does not
	// know, and cannot know by looking at itself.
	takeoverAt := fixedInstant.Add(time.Minute)
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, err := manager.Acquire(context.Background(), tx, lease.AcquireRequest{
			TenantID: tenantID, Resource: resource, Holder: fencedHolderB,
			Now: takeoverAt, TTL: time.Minute,
		})
		return err
	})

	sink := runtime.NewMemorySink()
	var sqls []string
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		rec := &recordingExecutor{tx: tx}
		_, advErr := runtime.AdvanceFenced(context.Background(), rec, runtime.FencedAdvanceRequest{
			Fence:    fenceOf(first, takeoverAt.Add(time.Second)),
			Verifier: lease.Fenced{Manager: manager},
			Request: runtime.AdvanceRequest{
				TenantID: tenantID, InstanceID: start.InstanceID,
				ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1, Plan: pf.Plan,
				Outcome: frontier.NodeOutcome{
					NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded,
					OutputDigest: "sha256:snapshot",
				},
				RecordedAt: fixedInstant, Sink: sink,
			},
		})
		sqls = rec.statements()
		return advErr
	})
	if err == nil {
		t.Fatal("a superseded holder advanced the instance")
	}
	if got := runtime.CodeOf(err); got != runtime.CodeFenceRefused {
		t.Fatalf("code = %q (%v), want %q", got, err, runtime.CodeFenceRefused)
	}
	// The lease package's own classification survives the wrap, so a caller
	// can still tell a stale fence from a lost lease.
	if !errors.Is(err, lease.ErrFenceStale) {
		t.Fatalf("refusal does not classify as lease.ErrFenceStale: %v", err)
	}
	if got := lease.CodeOf(err); got != lease.CodeFenceStale {
		t.Fatalf("wrapped lease code = %q, want %q", got, lease.CodeFenceStale)
	}

	// The ordering claim: the refusal happened before any workflow state was
	// read, and certainly before any was written.
	if table := touchedWorkflowState(sqls); table != "" {
		t.Fatalf("a refused fenced advancement still touched %s: %v", table, sqls)
	}
	if len(sink.Records()) != 0 {
		t.Fatalf("a refused fenced advancement dispatched %d continuations", len(sink.Records()))
	}

	// And the instance is where it was.
	var loaded runtime.Instance
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var lerr error
		loaded, lerr = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, start.InstanceID)
		return lerr
	})
	if loaded.InstanceVersion != start.InstanceVersion {
		t.Fatalf("instance version moved from %d to %d under a refused advancement",
			start.InstanceVersion, loaded.InstanceVersion)
	}
}

// A malformed fence or a missing verifier is refused as a value, so it costs
// no statement at all -- not even the lease read.
func TestAdvanceFencedRefusesAMalformedFenceWithoutAnyStatement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tenantID := uuid.New()
	instanceID := uuid.New()

	base := runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: instanceID, ExpectedInstanceVersion: 1, Attempt: 1,
		Plan:       referencePlan(t),
		Outcome:    frontier.NodeOutcome{NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded},
		RecordedAt: fixedInstant, Sink: runtime.NewMemorySink(),
	}
	good := runtime.Fence{
		ResourceKind: lease.ResourceWorkflowInstance, ResourceID: instanceID.String(),
		LeaseID: uuid.New(), HolderID: "workload:w#replica:1", Token: 1, At: fixedInstant,
	}

	// No verifier at all.
	if _, err := runtime.AdvanceFenced(ctx, panicExecutor{t: t}, runtime.FencedAdvanceRequest{
		Fence: good, Request: base,
	}); runtime.CodeOf(err) != runtime.CodeFenceRequired {
		t.Fatalf("no verifier: code = %q (%v), want %q", runtime.CodeOf(err), err, runtime.CodeFenceRequired)
	}

	verifier := refusingVerifier{err: errors.New("must not be called")}
	for name, mutate := range map[string]func(f *runtime.Fence){
		"no resource kind": func(f *runtime.Fence) { f.ResourceKind = "" },
		"no resource id":   func(f *runtime.Fence) { f.ResourceID = "" },
		"no lease id":      func(f *runtime.Fence) { f.LeaseID = uuid.Nil },
		"no holder":        func(f *runtime.Fence) { f.HolderID = "" },
		"zero token":       func(f *runtime.Fence) { f.Token = 0 },
		"no instant":       func(f *runtime.Fence) { f.At = time.Time{} },
	} {
		fence := good
		mutate(&fence)
		_, err := runtime.AdvanceFenced(ctx, panicExecutor{t: t}, runtime.FencedAdvanceRequest{
			Fence: fence, Verifier: verifier, Request: base,
		})
		if got := runtime.CodeOf(err); got != runtime.CodeFenceRequired {
			t.Fatalf("%s: code = %q (%v), want %q", name, got, err, runtime.CodeFenceRequired)
		}
	}
}

// panicExecutor fails the test if any statement runs through it.
type panicExecutor struct{ t *testing.T }

func (e panicExecutor) Exec(_ context.Context, sql string, _ ...any) (int64, error) {
	e.t.Fatalf("a value-level refusal still issued a statement: %s", sql)
	return 0, nil
}

func (e panicExecutor) Query(_ context.Context, sql string, _ ...any) (dbport.Rows, error) {
	e.t.Fatalf("a value-level refusal still issued a query: %s", sql)
	return nil, nil
}

func (e panicExecutor) QueryRow(_ context.Context, sql string, _ ...any) dbport.Row {
	e.t.Fatalf("a value-level refusal still issued a query: %s", sql)
	return nil
}

var _ runtime.Executor = panicExecutor{}

func containsSQL(sqls []string, needle string) bool {
	for _, sql := range sqls {
		if strings.Contains(sql, needle) {
			return true
		}
	}
	return false
}
