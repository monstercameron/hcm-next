package artifacts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// --- database plumbing (the same shape internal/workflow/migrate's own
// fixtures_test.go uses; those helpers are unexported to that package and
// cannot be reused across a package boundary) --------------------------------

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

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

// --- a real paused Promotion instance ----------------------------------------

// promotionEnvironment is one tenant holding one PAUSED Promotion instance
// with a live lease, a pending timer on its frontier node, an open signal
// subscription, an unsettled ready-work row, a live approval work item on the
// approval node, and one awaited child instance.
type promotionEnvironment struct {
	db   *pgtest.DB
	conn *pgxadapter.Conn

	tenant   uuid.UUID
	instance uuid.UUID
	child    uuid.UUID

	plan *workflow.CompiledWorkflow

	holder  lease.Identity
	fence   lease.Fence
	timerID uuid.UUID
	subID   uuid.UUID
	readyID uuid.UUID
	workID  uuid.UUID
}

func newPromotionEnvironment(t *testing.T) *promotionEnvironment {
	t.Helper()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "wfrun026")
	conn := appConn(t, db)

	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	plan, err := workflow.CompilePromotionReference(registry)
	if err != nil {
		t.Fatalf("compile Promotion fixture: %v", err)
	}

	env := &promotionEnvironment{
		db: db, conn: conn, tenant: tenantID, plan: plan,
		instance: uuid.New(), child: uuid.New(),
		holder: lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica-1"},
	}

	store := runtime.Store{}
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		for _, id := range []uuid.UUID{env.instance, env.child} {
			inst, err := runtime.NewInstance(tenantID, id, "cell-local", plan,
				workflow.ModeSimulate, "sha256:input-fixture", "corr-"+id.String(), fixedInstant)
			if err != nil {
				return err
			}
			if _, err := store.CreateInstance(ctx, tx, inst); err != nil {
				return err
			}
		}
		return nil
	})

	// CREATED -> PAUSE_REQUESTED -> PAUSED is the state machine's own route to
	// a paused instance; there is no shortcut and this fixture does not invent
	// one.
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		version := int64(1)
		for _, status := range []runtime.InstanceStatus{runtime.InstancePauseRequested, runtime.InstancePaused} {
			updated, err := store.RecordInstanceState(ctx, tx, runtime.InstanceTransition{
				TenantID: tenantID, InstanceID: env.instance, ExpectedVersion: version,
				Status: status, CurrentNodeIDs: []string{workflow.PromotionNodeBuildProposal},
			})
			if err != nil {
				return err
			}
			version = updated.InstanceVersion
		}
		return nil
	})

	env.seedLease(t)
	env.seedSchedulingArtifacts(t)
	env.seedApproval(t)
	env.seedChildLink(t)
	return env
}

func (e *promotionEnvironment) seedLease(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var grant lease.Grant
	inTenantTx(t, e.conn, e.tenant, func(tx dbport.Tx) error {
		var err error
		grant, err = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: e.tenant,
			Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: e.instance.String()},
			Holder:   e.holder, Now: fixedInstant, TTL: 2 * time.Hour,
		})
		return err
	})
	e.fence = grant.Fence
}

func (e *promotionEnvironment) seedSchedulingArtifacts(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	e.timerID = timer.TimerID(e.tenant, e.instance, workflow.PromotionNodeBuildProposal, sourceRequirementDigest)
	e.subID = SubscriptionID(e.tenant, e.instance, workflow.PromotionNodeBuildProposal, "promotion.finance_ack")
	e.readyID = timer.ReadyWorkID(e.tenant, e.instance, workflow.PromotionNodeBuildProposal, 1)

	inTenantTx(t, e.conn, e.tenant, func(tx dbport.Tx) error {
		if err := (runtimestate.TimerStore{}).Set(ctx, tx, runtimestate.Timer{
			TenantID: e.tenant, TimerID: e.timerID, InstanceID: e.instance,
			NodeID: workflow.PromotionNodeBuildProposal, Key: sourceRequirementDigest,
			Kind: runtimestate.TimerDelay, FiresAt: wakeInstant, CreatedAt: fixedInstant,
		}); err != nil {
			return err
		}
		if err := (runtimestate.SignalStore{}).Subscribe(ctx, tx, runtimestate.Subscription{
			TenantID: e.tenant, SubscriptionID: e.subID, InstanceID: e.instance,
			NodeID: workflow.PromotionNodeBuildProposal, SignalName: "promotion.finance_ack",
			CorrelationKey: "employment:jane-doe-9001", CreatedAt: fixedInstant,
		}); err != nil {
			return err
		}
		return (runtimestate.ReadyWorkStore{}).Enqueue(ctx, tx, runtimestate.ReadyWork{
			TenantID: e.tenant, ReadyWorkID: e.readyID, InstanceID: e.instance,
			NodeID: workflow.PromotionNodeBuildProposal, Attempt: 1,
			State: runtimestate.ReadyReady, EligibleAt: wakeInstant, EnqueuedAt: fixedInstant,
		})
	})
}

func (e *promotionEnvironment) seedApproval(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	item, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
		TenantID: e.tenant, Kind: workitem.KindApproval, WorkType: "promotion.finance_approval",
		CorrelationID: "corr-" + e.instance.String(), WorkflowInstanceID: e.instance,
		NodeID: workflow.PromotionNodeEndApproval, ProposalRef: "proposal:promotion-1",
		SubjectRefs:    []string{"employment:jane-doe-9001"},
		PolicyRouteRef: "route:finance-approval", Visibility: workitem.VisibilityOrganizationScope,
		OrganizationScopeID: "org:acme-test:eng",
		DeadlineAt:          fixedInstant.Add(48 * time.Hour), CreatedAt: fixedInstant,
	}, "requirement:finance-approval")
	if err != nil {
		t.Fatalf("build the approval work item: %v", err)
	}
	e.workID = item.WorkItemID
	inTenantTx(t, e.conn, e.tenant, func(tx dbport.Tx) error {
		_, err := (workitem.Store{}).Create(ctx, tx, item, workitem.TransitionMeta{
			ActorPrincipalID: "principal:runtime", Reason: "work_item_required", At: fixedInstant,
		})
		return err
	})
}

func (e *promotionEnvironment) seedChildLink(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	inTenantTx(t, e.conn, e.tenant, func(tx dbport.Tx) error {
		return (runtimestate.ChildLinkStore{}).Link(ctx, tx, runtimestate.ChildLink{
			TenantID: e.tenant, Parent: e.instance, Child: e.child,
			ParentNodeID: workflow.PromotionNodeSimulateComp, Ordinal: 1,
			Mode: runtimestate.ChildAwait, InputDigest: placeholderDigest, CreatedAt: fixedInstant,
		})
	})
}

// scope builds the migration frame: the instance's real epoch moving onto a
// bumped workflow version, relocating the frontier from build_proposal to
// raise_threshold with a replacement wake requirement for its timer.
func (e *promotionEnvironment) scope() Scope {
	return Scope{
		TenantID: e.tenant, InstanceID: e.instance,
		From: Epoch{
			WorkflowVersion: e.plan.Version, CompiledPlanDigest: e.plan.Digest(),
			NodeID: workflow.PromotionNodeBuildProposal, Attempt: 1,
		},
		To: Epoch{
			WorkflowVersion: e.plan.Version + 1, CompiledPlanDigest: "sha256:target-plan-fixture",
			NodeID: workflow.PromotionNodeRaiseThreshold, Attempt: 1,
		},
		Fence: e.fence,
		Requirements: map[string]wait.TimerRequirement{
			sourceRequirementDigest: targetRequirementFor(workflow.PromotionNodeRaiseThreshold, wakeInstant),
		},
		MigratedBy: "principal:migration-operator", MigratedAt: fixedInstant,
	}
}

func (e *promotionEnvironment) countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	e.db.QueryRow(context.Background(), query, args...).Scan(&n)
	return n
}

// placeholderDigest is a syntactically valid 64-hex content digest used
// wherever a fixture must supply one but its exact value is not under test.
const placeholderDigest = "0000000000000000000000000000000000000000000000000000000000000000"
