package runtimestate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps.
var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

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
// least-privilege hcmnext_app role.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// inTx runs fn inside its own transaction on conn and commits it.
func inTx(t *testing.T, conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTxErr(conn, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

// inTxErr is inTx for a call whose own error the test wants to inspect.
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

// inTenantTx is inTx with the tenant scope set as its first statement.
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

// repeatHex builds a 64-character hex string.
func repeatHex(digit string) string {
	out := ""
	for range 64 {
		out += digit
	}
	return out
}

// digestOf returns a well-formed canonical request digest over seed.
func digestOf(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// defaultPolicy is a retention policy every idempotency test that is not
// itself exercising the retention RED case can reuse.
var defaultPolicy = idempotency.RetentionPolicy{
	Retention:   30 * 24 * time.Hour,
	RetryWindow: 24 * time.Hour,
}

// --- internal/workflow/runtime fixtures -------------------------------

// referencePlan compiles the promotion reference workflow under the
// exceeds-threshold scenario, the same walk WF-RUN-025's own tests drive.
func referencePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(simulate.PromotionExceedsThresholdPay)
	if err != nil {
		t.Fatalf("compile the promotion reference: %v", err)
	}
	return setup.Plan
}

// newInstance builds a CREATED instance for tenant against plan.
func newInstance(t *testing.T, tenant uuid.UUID, plan *workflow.CompiledWorkflow) runtime.Instance {
	t.Helper()
	inst, err := runtime.NewInstance(
		tenant, uuid.New(), "cell-local", plan, workflow.ModeSimulate,
		"sha256:"+repeatHex("1"), "corr-"+tenant.String(), fixedInstant)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	return inst
}

// createInstance persists a fresh instance and records its start node's
// initial READY attempt, leaving it exactly where runtime.Start would --
// without pulling in Start's own proposal/version/resolver machinery.
func createInstance(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant uuid.UUID, plan *workflow.CompiledWorkflow) runtime.Instance {
	t.Helper()
	store := runtime.Store{}
	inst := newInstance(t, tenant, plan)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.CreateInstance(ctx, tx, inst)
		return err
	})
	startNode, ok := plan.Node(plan.StartNodeID)
	if !ok {
		t.Fatalf("compiled plan declares no start node %q", plan.StartNodeID)
	}
	ne := runtime.NewNodeExecution(tenant, inst.InstanceID, plan.StartNodeID, 1, startNode.Type, runtime.NodeReady)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, _, err := store.RecordNodeExecution(ctx, tx, ne, 1)
		return err
	})
	var reloaded runtime.Instance
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		reloaded, err = store.LoadInstance(ctx, tx, tenant, inst.InstanceID)
		return err
	})
	return reloaded
}

// advanceOn runs one runtime.Advance call in its own transaction on conn.
func advanceOn(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, req runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
	t.Helper()
	var receipt runtime.AdvanceReceipt
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		var advErr error
		receipt, advErr = runtime.Advance(context.Background(), tx, req)
		return advErr
	})
	return receipt, err
}

// failingSink is a runtime.ContinuationSink that fails on exactly one
// declared intent kind, mirroring WF-RUN-025's own failpoint fixture.
type failingSink struct {
	failOn frontier.IntentKind
}

func (f failingSink) fail(kind frontier.IntentKind) error {
	if kind == f.failOn {
		return fmt.Errorf("injected continuation failure for %s", kind)
	}
	return nil
}
func (f failingSink) RequireWorkItem(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentWorkItemRequired)
}
func (f failingSink) RequireSignalSubscription(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentSignalSubscriptionRequired)
}
func (f failingSink) RequireTimer(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentTimerRequired)
}
func (f failingSink) MarkReady(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentReady)
}
func (f failingSink) Complete(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentComplete)
}

var _ runtime.ContinuationSink = failingSink{}

// crossStoreSink is DB-012's own composed runtime.ContinuationSink: every
// intent kind delegates to runtime.ContinuationStore's audit-only ledger,
// and a WORK_ITEM_REQUIRED intent additionally creates a real, durable
// WorkItem through internal/humanwork/workitem -- the governed state
// runtime.ContinuationStore deliberately does not create itself. Every write
// goes through the same ex the caller transaction is fencing, so runtime,
// workitem and continuation state commit or roll back together.
//
// RequireWorkItem also guards DB-012's duplicate-approval-slot RED case: it
// checks whether this exact continuation identity already exists before
// creating a WorkItem, so dispatching the same WORK_ITEM_REQUIRED intent
// twice creates exactly one WorkItem, not two competing approval slots for
// the same node attempt.
type crossStoreSink struct {
	items workitem.Store
	at    time.Time
}

func (s crossStoreSink) RequireWorkItem(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	id := runtime.ContinuationID(rec.TenantID, rec.InstanceID, rec.SourceNodeID, rec.SourceAttempt, rec.TargetNodeID, rec.Kind)
	var already bool
	if err := ex.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM workflow_continuation WHERE tenant_id = $1 AND continuation_id = $2)`,
		rec.TenantID, id).Scan(&already); err != nil {
		return err
	}
	if err := (runtime.ContinuationStore{}).RequireWorkItem(ctx, ex, rec); err != nil {
		return err
	}
	if already {
		return nil
	}
	item, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
		TenantID:            rec.TenantID,
		Kind:                workitem.KindTask,
		WorkType:            "worktype.db012.review/v1",
		CorrelationID:       "corr-" + rec.InstanceID.String(),
		WorkflowInstanceID:  rec.InstanceID,
		NodeID:              rec.TargetNodeID,
		SubjectRefs:         []string{"worker:jane"},
		PolicyRouteRef:      "route.db012.current_manager/v1",
		Visibility:          workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: "org:acme-test:eng",
		DeadlineAt:          s.at.Add(48 * time.Hour),
		CreatedAt:           s.at,
	})
	if err != nil {
		return err
	}
	_, err = s.items.Create(ctx, ex, item, workitem.TransitionMeta{
		ActorPrincipalID: "principal:db012-frontier", Reason: workitem.ReasonCreated, At: s.at,
	})
	return err
}

func (s crossStoreSink) RequireSignalSubscription(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	return (runtime.ContinuationStore{}).RequireSignalSubscription(ctx, ex, rec)
}
func (s crossStoreSink) RequireTimer(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	return (runtime.ContinuationStore{}).RequireTimer(ctx, ex, rec)
}
func (s crossStoreSink) MarkReady(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	return (runtime.ContinuationStore{}).MarkReady(ctx, ex, rec)
}
func (s crossStoreSink) Complete(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	return (runtime.ContinuationStore{}).Complete(ctx, ex, rec)
}

var _ runtime.ContinuationSink = crossStoreSink{}

// --- internal/humanwork/workitem fixtures -------------------------------

// taskInputFor builds a well-formed workitem.NewWorkItemInput for a plain
// TASK work item attached to instance.
func taskInputFor(tenant, instance uuid.UUID, nodeID string) workitem.NewWorkItemInput {
	return workitem.NewWorkItemInput{
		TenantID:            tenant,
		Kind:                workitem.KindTask,
		WorkType:            "worktype.db012.review/v1",
		CorrelationID:       "corr-" + instance.String(),
		WorkflowInstanceID:  instance,
		NodeID:              nodeID,
		SubjectRefs:         []string{"worker:jane"},
		PolicyRouteRef:      "route.db012.current_manager/v1",
		Visibility:          workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: "org:acme-test:eng",
		DeadlineAt:          fixedInstant.Add(48 * time.Hour),
		CreatedAt:           fixedInstant,
	}
}

func workMeta(reason string) workitem.TransitionMeta {
	return workitem.TransitionMeta{
		ActorPrincipalID: "principal:db012-test",
		Reason:           reason,
		At:               fixedInstant,
	}
}

// The lifecycle fixtures below need a humanwork.Resolution to route against
// without themselves testing resolution.

func singleCandidateResolution(principal string) humanwork.Resolution {
	return humanwork.Resolution{
		RequirementID:     "req.db012/v1",
		Outcome:           humanwork.OutcomeResolved,
		Candidates:        []humanwork.Candidate{{PrincipalID: principal, Via: humanwork.SourceDirect, TermRef: "term.db012"}},
		ResolvedAt:        values.NewInstant(fixedInstant),
		EffectiveAt:       values.NewInstant(fixedInstant),
		DirectoryVersion:  "directory.db012/1",
		ExpressionDigest:  "sha256:" + repeatHex("2"),
		RequirementDigest: "sha256:" + repeatHex("3"),
		QuorumRequired:    1,
	}
}

func multiCandidateResolution(principals ...string) humanwork.Resolution {
	sorted := append([]string(nil), principals...)
	sort.Strings(sorted)
	candidates := make([]humanwork.Candidate, len(sorted))
	for i, p := range sorted {
		candidates[i] = humanwork.Candidate{PrincipalID: p, Via: humanwork.SourceDirect, TermRef: "term.db012"}
	}
	return humanwork.Resolution{
		RequirementID:     "req.db012/v1",
		Outcome:           humanwork.OutcomeResolved,
		Candidates:        candidates,
		ResolvedAt:        values.NewInstant(fixedInstant),
		EffectiveAt:       values.NewInstant(fixedInstant),
		DirectoryVersion:  "directory.db012/1",
		ExpressionDigest:  "sha256:" + repeatHex("4"),
		RequirementDigest: "sha256:" + repeatHex("5"),
		QuorumRequired:    1,
	}
}

// availableItem creates and routes a work item to ASSIGNED for a single
// named principal against instance, ready to be claimed.
func availableItem(t *testing.T, ctx context.Context, store workitem.Store, conn *pgxadapter.Conn, tenant, instance uuid.UUID, nodeID, owner string) workitem.WorkItem {
	t.Helper()
	in, err := workitem.NewWorkItem(taskInputFor(tenant, instance, nodeID))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	var item workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, workMeta(workitem.ReasonCreated))
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: singleCandidateResolution(owner), Trigger: workitem.TriggerInitialRouting},
			workMeta("workitem.routed"))
		return err
	})
	return item
}

// multiCandidateAvailableItem is availableItem's AVAILABLE-status sibling: it
// routes against more than one candidate, so any of them may claim it.
func multiCandidateAvailableItem(t *testing.T, ctx context.Context, store workitem.Store, conn *pgxadapter.Conn, tenant, instance uuid.UUID, nodeID string, principals ...string) workitem.WorkItem {
	t.Helper()
	in, err := workitem.NewWorkItem(taskInputFor(tenant, instance, nodeID))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	var item workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, workMeta(workitem.ReasonCreated))
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: multiCandidateResolution(principals...), Trigger: workitem.TriggerInitialRouting},
			workMeta("workitem.routed"))
		return err
	})
	return item
}
