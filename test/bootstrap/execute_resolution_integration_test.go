package bootstrap_test

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// commitOutcomeDB models the only failure this test is allowed to inject: the
// database has accepted COMMIT, but the caller receives an ambiguous error.
// It delegates every statement to the real pgx pool and is therefore not a
// workflow or platform fake.
type commitOutcomeDB struct {
	inner  execute.Beginner
	serial interface {
		BeginSerializable(context.Context) (dbport.Tx, error)
	}
	readonly interface {
		BeginReadOnly(context.Context) (dbport.Tx, error)
	}
	loseCommit   bool
	rollbackOnly bool
	serialBegins int
}

type commitOutcomeTx struct {
	dbport.Tx
	db *commitOutcomeDB
}

func (d *commitOutcomeDB) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := d.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &commitOutcomeTx{Tx: tx, db: d}, nil
}
func (d *commitOutcomeDB) BeginSerializable(ctx context.Context) (dbport.Tx, error) {
	d.serialBegins++
	tx, err := d.serial.BeginSerializable(ctx)
	if err != nil {
		return nil, err
	}
	return &commitOutcomeTx{Tx: tx, db: d}, nil
}
func (d *commitOutcomeDB) BeginReadOnly(ctx context.Context) (dbport.Tx, error) {
	return d.readonly.BeginReadOnly(ctx)
}
func (t *commitOutcomeTx) Commit(ctx context.Context) error {
	if t.db.rollbackOnly {
		if err := t.Tx.Rollback(ctx); err != nil {
			return err
		}
		return commit.ErrCommitAmbiguous
	}
	if err := t.Tx.Commit(ctx); err != nil {
		return err
	}
	if t.db.loseCommit {
		t.db.loseCommit = false
		return commit.ErrCommitAmbiguous
	}
	return nil
}

var _ execute.Beginner = (*commitOutcomeDB)(nil)
var _ execute.SerializableBeginner = (*commitOutcomeDB)(nil)
var _ execute.ReadOnlyBeginner = (*commitOutcomeDB)(nil)

func TestTodo_DBEDGE003_ExecuteIntentResolvesLostCommitWithoutReplay(t *testing.T) {
	terminal := &recordingTerminalWriter{}
	var db *commitOutcomeDB
	c := newExecutionCellWithDB(t, terminal, func(pool *pgxadapter.Pool) execute.Beginner {
		db = &commitOutcomeDB{inner: pool, serial: pool, readonly: pool, loseCommit: true}
		return db
	})
	seedWorkforce(t, c)
	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx := c.grpcContext(callCtx)

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "dbedge003-resolve"))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	simulated, err := c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: created.GetIntent().GetIntentId()})
	if err != nil {
		t.Fatalf("SimulateIntent: %v", err)
	}
	got, err := c.grpcIntent.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{
		IdempotencyKey: "dbedge003-resolve", IntentId: created.GetIntent().GetIntentId(),
		ExpectedInstanceVersion: created.GetIntent().GetInstanceVersion(),
		Approval:                &intentsv1.ProposalApproval{ProposalRevisionId: simulated.GetSimulation().GetProposalRevisionId(), MaterialProposalDigest: simulated.GetSimulation().GetMaterialProposalDigest(), Approved: true, ApprovalRef: "approval:dbedge003-resolve"},
	})
	if err != nil {
		t.Fatalf("ExecuteIntent after committed-but-lost response: %v", err)
	}
	if got.GetExecution().GetStatus() != intentsv1.ExecutionReceiptStatus_EXECUTION_RECEIPT_STATUS_RESOLVED {
		t.Fatalf("status = %s, want RESOLVED", got.GetExecution().GetStatus())
	}
	resolved := got.GetExecution().GetResolvedStart()
	if resolved == nil || resolved.GetRuntimeStatus() != string(runtime.InstanceCreated) ||
		resolved.GetWorkflowId() == "" || resolved.GetWorkflowVersion() == 0 ||
		resolved.GetCompiledPlanDigest() == "" || resolved.GetSemanticVersion() != "1.0.0" ||
		len(resolved.GetCurrentNodeIds()) != 1 {
		t.Fatalf("resolved start = %+v, want durable current state", got.GetExecution().GetResolvedStart())
	}
	if got.GetExecution().GetInstanceId() == "" || got.GetExecution().GetInstanceVersion() == 0 ||
		len(got.GetExecution().GetVisitedNodes()) != 0 || len(got.GetExecution().GetWorkItems()) != 0 {
		t.Fatalf("resolved receipt fabricated replay output: %+v", got.GetExecution())
	}
	if db.serialBegins != 1 {
		t.Fatalf("serial START transactions = %d, want one despite retry budget", db.serialBegins)
	}
	if len(terminal.Calls()) != 0 {
		t.Fatalf("terminal writes = %d, want zero", len(terminal.Calls()))
	}

	tenantID := pgstore.TenantID(testTenant)
	var instanceID, workflowID, planHash string
	var workflowVersion int32
	var instanceVersion int64
	var frontier []string
	if err := c.db.QueryRow(callCtx, `SELECT instance_id::text, workflow_id, workflow_version, compiled_plan_hash, instance_version, current_node_ids FROM workflow_instance WHERE tenant_id=$1`, tenantID).
		Scan(&instanceID, &workflowID, &workflowVersion, &planHash, &instanceVersion, &frontier); err != nil {
		t.Fatalf("load resolved instance: %v", err)
	}
	if got.GetExecution().GetInstanceId() != instanceID || resolved.GetWorkflowId() != workflowID ||
		resolved.GetWorkflowVersion() != uint32(workflowVersion) || resolved.GetCompiledPlanDigest() != planHash ||
		got.GetExecution().GetInstanceVersion() != uint64(instanceVersion) || len(frontier) != 1 || frontier[0] != resolved.GetCurrentNodeIds()[0] {
		t.Fatalf("wire resolution does not match durable instance: execution=%+v resolved=%+v", got.GetExecution(), resolved)
	}
	var nodes, workItems int
	if err := c.db.QueryRow(callCtx, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1`, tenantID).Scan(&nodes); err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if nodes == 0 {
		t.Fatal("workflow node executions = 0, want the durable start frontier")
	}
	if err := c.db.QueryRow(callCtx, `SELECT count(*) FROM work_item WHERE tenant_id=$1`, tenantID).Scan(&workItems); err != nil {
		t.Fatalf("count work items: %v", err)
	}
	if workItems != 0 {
		t.Fatalf("work items = %d, want zero during outcome resolution", workItems)
	}
}

func TestTodo_DBEDGE003_UnresolvedCommitRollsBackAndRetainsTypedError(t *testing.T) {
	terminal := &recordingTerminalWriter{}
	var db *commitOutcomeDB
	c := newExecutionCellWithDB(t, terminal, func(pool *pgxadapter.Pool) execute.Beginner {
		db = &commitOutcomeDB{inner: pool, serial: pool, readonly: pool, rollbackOnly: true}
		return db
	})
	seedWorkforce(t, c)
	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx := c.grpcContext(callCtx)
	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "dbedge003-rollback"))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	simulated, err := c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: created.GetIntent().GetIntentId()})
	if err != nil {
		t.Fatalf("SimulateIntent: %v", err)
	}
	_, err = c.grpcIntent.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{IdempotencyKey: "dbedge003-rollback", IntentId: created.GetIntent().GetIntentId(), ExpectedInstanceVersion: created.GetIntent().GetInstanceVersion(), Approval: &intentsv1.ProposalApproval{ProposalRevisionId: simulated.GetSimulation().GetProposalRevisionId(), MaterialProposalDigest: simulated.GetSimulation().GetMaterialProposalDigest(), Approved: true, ApprovalRef: "approval:dbedge003-rollback"}})
	if err == nil {
		t.Fatal("unresolved commit must return an error")
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("error code = %s, want unavailable ambiguity: %v", status.Code(err), err)
	}
	st := status.Convert(err)
	var detail *commonv1.ErrorDetail
	for _, candidate := range st.Details() {
		if typed, ok := candidate.(*commonv1.ErrorDetail); ok {
			detail = typed
		}
	}
	if detail == nil || detail.GetReasonRef() != "intent.execution_outcome_ambiguous" || detail.GetRetryable() {
		t.Fatalf("structured ambiguity detail = %+v", detail)
	}
	if len(terminal.Calls()) != 0 {
		t.Fatalf("terminal writes = %d, want zero", len(terminal.Calls()))
	}
	if db.serialBegins != 1 {
		t.Fatalf("serial START transactions = %d, want one despite retry budget", db.serialBegins)
	}
	tenantID := pgstore.TenantID(testTenant)
	var instances, nodes, workItems int
	if err := c.db.QueryRow(callCtx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, tenantID).Scan(&instances); err != nil {
		t.Fatalf("count instances: %v", err)
	}
	if instances != 0 {
		t.Fatalf("workflow instances = %d, want zero after rollback", instances)
	}
	if err := c.db.QueryRow(callCtx, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id=$1`, tenantID).Scan(&nodes); err != nil {
		t.Fatalf("count workflow_node_execution: %v", err)
	}
	if err := c.db.QueryRow(callCtx, `SELECT count(*) FROM work_item WHERE tenant_id=$1`, tenantID).Scan(&workItems); err != nil {
		t.Fatalf("count work_item: %v", err)
	}
	if nodes != 0 || workItems != 0 {
		t.Fatalf("rows after rollback: nodes=%d work_items=%d, want zero", nodes, workItems)
	}
}
