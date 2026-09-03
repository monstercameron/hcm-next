package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	adminv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transport/admin"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestMain runs pgtest's embedded PostgreSQL lifecycle for this package's one
// DB-backed test. No other file in this package needs a database: every
// other AdminService method is exercised over fakes.
func TestMain(m *testing.M) { pgtest.RunMain(m) }

// admin008FixedInstant is the clock every fixture below stamps; this test
// never reads a wall clock.
var admin008FixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// insertAdmin008Tenant registers one active tenant as the pgtest superuser
// connection, matching the identical helper every workflow-runtime and
// work-item package test uses.
func insertAdmin008Tenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func admin008InTx(t *testing.T, conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestTodo_ADMIN_008_Integration proves GetWorkflowInstance end to end
// against a real PostgreSQL instance, work item and transition, driven
// through the generated gRPC client exactly like a real hcmctl invocation
// would: internal/transport/admin composes internal/workflow/runtime.Store
// and internal/humanwork/workitem.Store over one pooled connection and hands
// their loaded rows to internal/workflow/inspect, and this test never reads
// a runtime or work-item table itself -- it only asserts on the wire
// response, which is the whole of ADMIN-008's "hcmctl cannot show a run
// today" RED case being fixed.
func TestTodo_ADMIN_008_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertAdmin008Tenant(t, db, "admin008-integration")
	conn := db.NewConn(t)

	setup, err := simulate.NewPromotionSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatalf("NewPromotionSetup: %v", err)
	}
	plan := setup.Plan
	instanceID := uuid.New()

	inst, err := runtime.NewInstance(tenant, instanceID, "cell-local", plan, workflow.ModeSimulate,
		"artifact:input/admin008", "corr-admin008", admin008FixedInstant)
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	// Move the fixture straight to RUNNING with one lifecycle dimension set,
	// so the rendered view has something other than defaults to assert on.
	inst.RuntimeStatus = runtime.InstanceRunning
	inst.StartedAt = &admin008FixedInstant
	inst.CompletionDimensions = runtime.Dimensions{
		RequestState: "APPROVED", ExecutionState: "IN_PROGRESS",
		BusinessState: "IN_PROGRESS", ConsistencyState: "CONSISTENT", ObligationState: "PENDING",
	}

	admin008InTx(t, conn, func(tx dbport.Tx) error {
		_, err := (runtime.Store{}).CreateInstance(ctx, tx, inst)
		return err
	})

	node := runtime.NewNodeExecution(tenant, instanceID, plan.StartNodeID, 1, workflow.StepTransform, runtime.NodeSucceeded)
	node.OutputArtifactRef = "artifact:output/admin008"
	node.RecordedAt = admin008FixedInstant
	admin008InTx(t, conn, func(tx dbport.Tx) error {
		_, _, err := (runtime.Store{}).RecordNodeExecution(ctx, tx, node, inst.InstanceVersion)
		return err
	})

	approval, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
		TenantID:            tenant,
		WorkType:            "promotion.finance_approval",
		CorrelationID:       "corr-admin008",
		WorkflowInstanceID:  instanceID,
		NodeID:              plan.StartNodeID,
		SubjectRefs:         []string{"worker:jane"},
		PolicyRouteRef:      "route:finance",
		Visibility:          workitem.VisibilityOrganizationScope,
		OrganizationScopeID: "org:acme",
		DeadlineAt:          admin008FixedInstant.Add(48 * time.Hour),
		CreatedAt:           admin008FixedInstant,
	}, "approval-req:admin008")
	if err != nil {
		t.Fatalf("NewApprovalTask: %v", err)
	}
	admin008InTx(t, conn, func(tx dbport.Tx) error {
		_, err := (workitem.Store{}).Create(ctx, tx, approval, workitem.TransitionMeta{
			ActorPrincipalID: "principal:ops-7", Reason: workitem.ReasonCreated, At: admin008FixedInstant,
		})
		return err
	})

	deps := admin.Dependencies{
		WorkflowExecutor: conn,
		TenantUUID:       func(values.TenantId) string { return tenant.String() },
	}
	gconn, cleanup := startTestServer(t, deps)
	defer cleanup()
	client := dialAdminClient(gconn)
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.GetWorkflowInstance(withToken(cctx, fixtureOperatorToken), &adminv1.GetWorkflowInstanceRequest{
		InstanceId: instanceID.String(),
	})
	if err != nil {
		t.Fatalf("GetWorkflowInstance: %v", err)
	}
	if !resp.GetDisclosed() {
		t.Fatalf("response not disclosed: %+v", resp)
	}
	if resp.GetDefinition().GetWorkflowId() != plan.WorkflowID {
		t.Errorf("definition.workflow_id = %q, want %q", resp.GetDefinition().GetWorkflowId(), plan.WorkflowID)
	}
	if resp.GetInstance().GetRuntimeStatus() != "RUNNING" {
		t.Errorf("instance.runtime_status = %q, want RUNNING", resp.GetInstance().GetRuntimeStatus())
	}
	lc := resp.GetInstance().GetLifecycle()
	if lc.GetRequestState() != "APPROVED" || lc.GetObligationState() != "PENDING" {
		t.Errorf("lifecycle = %+v, want the five recorded dimensions", lc)
	}
	if len(resp.GetNodes()) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.GetNodes()))
	}
	node0 := resp.GetNodes()[0]
	if node0.GetOutputArtifactRef().GetState() != "VALUE" || node0.GetOutputArtifactRef().GetValue() != "artifact:output/admin008" {
		t.Errorf("node output_artifact_ref = %+v, want the recorded value", node0.GetOutputArtifactRef())
	}
	if !resp.GetWorkItemsDisclosed() {
		t.Fatal("work items section not disclosed")
	}
	if len(resp.GetWorkItems()) != 1 {
		t.Fatalf("work items = %d, want 1", len(resp.GetWorkItems()))
	}
	item := resp.GetWorkItems()[0]
	if item.GetKind() != "APPROVAL" {
		t.Errorf("work item kind = %q, want APPROVAL", item.GetKind())
	}
	if item.GetApprovalRequirementRef().GetState() != "VALUE" || item.GetApprovalRequirementRef().GetValue() != "approval-req:admin008" {
		t.Errorf("approval_requirement_ref = %+v, want the recorded evidence id", item.GetApprovalRequirementRef())
	}
	if !item.GetTransitionsRecorded() || len(item.GetTransitions()) != 1 {
		t.Fatalf("transitions = %+v, want exactly the CREATED row", item.GetTransitions())
	}
	if item.GetTransitions()[0].GetToStatus() != "CREATED" {
		t.Errorf("transition to_status = %q, want CREATED", item.GetTransitions()[0].GetToStatus())
	}
	if !resp.GetComplete() {
		t.Errorf("response reports incomplete with no denial and a full lifecycle: redactions=%v gaps=%v",
			resp.GetRedactions(), resp.GetGaps())
	}
	if resp.GetEvidenceRef().GetEvidenceId() == "" {
		t.Error("expected a non-empty evidence id")
	}

	t.Run("an unknown instance id is refused as not found, not a raw storage error", func(t *testing.T) {
		_, err := client.GetWorkflowInstance(withToken(cctx, fixtureOperatorToken), &adminv1.GetWorkflowInstanceRequest{
			InstanceId: uuid.New().String(),
		})
		assertOwnedCode(t, err, envelope.CodeNotFound)
	})
}
