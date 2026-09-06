// This file is OBS-023/OBS-024's own dedicated end-to-end run of the same
// demo graph TestPromotionWorkflowExecutesEndToEndWithOneGovernedWrite and
// TestTodo_WF_RUN_030 drive (deliberate duplication of the orchestration,
// not a refactor of either: this package's own established convention, see
// runPromotionToComplete's doc comment, is that each todo gets its own
// self-contained run so its test body stays independently safe to change).
// It differs from both only in what it wires into execute.Options:
// Instrumentation and Evidence, so it can assert the OBS-023 span/log chain
// and the OBS-024 evidence chain a plain runPromotionToComplete never
// exercises.
package workflow_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
	platformexecution "github.com/monstercameron/hcm-next/internal/platform/execution"
	"github.com/monstercameron/hcm-next/internal/platform/logging"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
	otelTestexport "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel/testexport"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry/testexport"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/execute/effects"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/hcm-next/internal/workflow/steps/approval"
	stepstask "github.com/monstercameron/hcm-next/internal/workflow/steps/task"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
)

// obsFixture is everything the OBS-023/OBS-024 integration tests read back
// after driving one promotion to COMPLETE with Instrumentation/Evidence
// wired in.
type obsFixture struct {
	db         *pgtest.DB
	tenantID   uuid.UUID
	instanceID uuid.UUID
	streamKey  string

	execResult    execute.Result
	approveToTask execute.Result
	taskToDone    execute.Result
}

// runPromotionWithTelemetry drives promotionApprovalTaskDefinition's demo
// graph from Execute through both governed WorkItems to COMPLETE, exactly as
// runPromotionToComplete does, with instrumentation and evidence wired into
// the driver.
func runPromotionWithTelemetry(
	t *testing.T, ctx context.Context, tenantKey string, at time.Time,
	instrumentation execute.Instrumentation, evidence execute.ExecutionEvidence,
) obsFixture {
	t.Helper()
	db := pgtest.New(t)
	beginner := appConn(t, db)
	tenantID := insertTenant(t, db, tenantKey, at)

	versions, plan, activated := publishActiveDemoPlan(t, at)
	proposal := newDemoProposal(t, values.TenantId(tenantKey), "intent:"+tenantKey, at)
	binding := runtime.ProposalBinding{Revision: proposal, Approved: true, ApprovalRef: "decision:hr-partner-approves-start"}
	managerReq, managerRes, _, _ := managerRequirementAndResolution(t)

	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: activated.CompiledPlanDigest}, Plan: plan,
	}}}
	terminal := &effects.LedgerTerminalWriter{
		Appender: newLedgerAppender(t), ProjectionName: "workflow_promotion_outcome_obs_" + tenantKey,
		SourceRef: "hcmnext:test:workflow:obs",
	}
	workItems := demoWorkItems{proposal: proposal, managerReq: managerReq, managerResolution: managerRes, taskOwner: humanwork.PrincipalHRBP}

	drv, err := execute.New(execute.Options{
		DB: beginner, Steps: endOnlySteps{}, WorkItems: workItems, Terminal: terminal,
		Items: workitem.Store{},
		Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:           func() time.Time { return at },
		Instrumentation: instrumentation, Evidence: evidence,
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}

	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:" + tenantKey,
		Resolver: resolver, Versions: versions, Proposal: binding,
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:" + tenantKey, CreatedAt: at,
	}

	parkedApproval, err := drv.Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if parkedApproval.Status != execute.StatusParked || len(parkedApproval.WorkItems) != 1 {
		t.Fatalf("Execute result = %+v, want PARKED with one WorkItem", parkedApproval)
	}
	approvalItem := parkedApproval.WorkItems[0]

	store := workitem.Store{}
	var completedApproval workitem.WorkItem
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		claimed, claimErr := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: approvalItem.WorkItemID, ExpectedVersion: approvalItem.ItemVersion,
			ClaimantPrincipalID: humanwork.PrincipalManager, ClaimExpiresAt: at.Add(time.Hour), Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalManager, Reason: "APPROVAL_CLAIMED", At: at},
		})
		if claimErr != nil {
			return claimErr
		}
		started, startErr := store.Start(ctx, tx, tenantID, approvalItem.WorkItemID, claimed.ItemVersion, at,
			workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalManager, Reason: "APPROVAL_STARTED", At: at})
		if startErr != nil {
			return startErr
		}
		decision := approvalDecisionFor(workitem.WorkItem{CompletedBy: humanwork.PrincipalManager}, managerReq, proposal, at)
		var completeErr error
		completedApproval, completeErr = stepsapproval.Complete(ctx, tx, store, started, decision, at,
			workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalManager, Reason: "APPROVAL_DECIDED", At: at})
		return completeErr
	})
	if completedApproval.Status != workitem.StatusCompleted {
		t.Fatalf("completed approval work item status = %s, want COMPLETED", completedApproval.Status)
	}

	approvalOut := approvalOutcome(t, completedApproval, managerReq, proposal, at, at.Add(time.Minute))
	parkedTask, err := drv.Resume(ctx, execute.ResumeRequest{
		Start: start, InstanceID: parkedApproval.Start.InstanceID, ExpectedInstanceVersion: parkedApproval.InstanceVersion,
		WorkItemID: completedApproval.WorkItemID, ExpectedWorkItemVersion: completedApproval.ItemVersion,
		Outcome: approvalOut, RecordedAt: at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Resume (approval->task): %v", err)
	}
	if parkedTask.Status != execute.StatusParked || len(parkedTask.WorkItems) != 1 {
		t.Fatalf("Resume result = %+v, want PARKED with one WorkItem", parkedTask)
	}
	taskItem := parkedTask.WorkItems[0]

	var completedTask workitem.WorkItem
	var submission stepstask.Submission
	claimID := uuid.New()
	claimExpires := at.Add(2 * time.Hour)
	submittedAt := at.Add(90 * time.Minute)
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		claimed, claimErr := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: taskItem.WorkItemID, ExpectedVersion: taskItem.ItemVersion,
			ClaimantPrincipalID: humanwork.PrincipalHRBP, ClaimExpiresAt: claimExpires, Now: at.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalHRBP, Reason: "TASK_CLAIMED", At: at.Add(time.Minute)},
		})
		if claimErr != nil {
			return claimErr
		}
		started, startErr := store.Start(ctx, tx, tenantID, taskItem.WorkItemID, claimed.ItemVersion, at.Add(2*time.Minute),
			workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalHRBP, Reason: "TASK_STARTED", At: at.Add(2 * time.Minute)})
		if startErr != nil {
			return startErr
		}
		claimed.ClaimID = &claimID
		started.ClaimID = &claimID
		var submitErr error
		completedTask, submission, submitErr = stepstask.Submit(ctx, tx, store, started, stepstask.SubmitInput{
			Node: demoTaskNode(),
			Spec: stepstask.SubmissionSpec{
				CompletedBy: humanwork.PrincipalHRBP, CandidateVia: humanwork.SourceDirect,
				ClaimID: claimID, ClaimExpiresAt: values.NewInstant(claimExpires), SubmittedAt: values.NewInstant(submittedAt),
				OutputSchema: demoTaskNode().OutputSchema, CanonicalPayloadDigest: "sha256:" + strings.Repeat("c", 64),
				FormDefinition: demoTaskNode().FormDefinition, RenderContextDigest: "sha256:" + strings.Repeat("d", 64),
				ValidationEvidenceRef: "evidence.validation.test/v1", AccessibilityEvidenceRef: "evidence.accessibility.test/v1",
				AccommodationEvidenceRef: "evidence.accommodation.test/v1",
			},
			Validator: stepstask.ValidatorFunc(func(stepstask.ValidationRequest) error { return nil }),
			Now:       submittedAt, Meta: workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalHRBP, Reason: "TASK_SUBMITTED", At: submittedAt},
		})
		return submitErr
	})
	if completedTask.Status != workitem.StatusCompleted {
		t.Fatalf("completed task work item status = %s, want COMPLETED", completedTask.Status)
	}

	taskOut := taskOutcome(t, completedTask, submission, submittedAt.Add(time.Minute))
	final, err := drv.Resume(ctx, execute.ResumeRequest{
		Start: start, InstanceID: parkedApproval.Start.InstanceID, ExpectedInstanceVersion: parkedTask.InstanceVersion,
		WorkItemID: completedTask.WorkItemID, ExpectedWorkItemVersion: completedTask.ItemVersion,
		Outcome: taskOut, RecordedAt: submittedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Resume (task->complete): %v", err)
	}
	if final.Status != execute.StatusComplete {
		t.Fatalf("final Resume result = %+v, want COMPLETE", final)
	}

	return obsFixture{
		db: db, tenantID: tenantID, instanceID: parkedApproval.Start.InstanceID,
		streamKey:     effects.StreamKeyFor(demoWorkflowID, parkedApproval.Start.InstanceID.String()),
		execResult:    parkedApproval,
		approveToTask: parkedTask,
		taskToDone:    final,
	}
}

// fixedObsTraceContext pins a parent trace/span so the driver's own child
// spans — and the trace id it stamps onto every node_execution row — are
// deterministic across a real Postgres run.
var fixedObsTraceID = trace.TraceID{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01}

// obsTestResource and obsTestEvaluator build the minimal, valid
// internal/platform/telemetry.Resource/Evaluator a real
// internal/platform/telemetry/otel.Provider requires — the same recipe
// that package's own test suite uses, duplicated here because a Go test
// cannot import another package's unexported test helpers.
func obsTestResource(t *testing.T) telemetry.Resource {
	t.Helper()
	res := telemetry.NewResourceFromBuild(
		buildinfo.Info{Revision: "abc123"},
		"hcm-workflow-obs-test", "instance-1", "test", "cell-p1a", "us-east-1",
		telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
	)
	if err := res.Validate(); err != nil {
		t.Fatalf("test resource does not validate: %v", err)
	}
	return res
}

func obsTestEvaluator(t *testing.T) *telemetry.Evaluator {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	return telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
}

func fixedObsTraceContext(ctx context.Context) context.Context {
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: fixedObsTraceID, SpanID: trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}, TraceFlags: trace.FlagsSampled, Remote: true,
	}))
}

// TestTodo_OBS_023_Integration proves OBS-023's GREEN clause against a real
// database: the driver propagates the incoming span context's trace id all
// the way into the durable node_execution rows Advance persists, opens a
// node span for the plan's TRANSFORM/END steps, an advance span per
// advancement and a terminal span for the governed write, and emits the one
// required log/slog line per advancement and per terminal write — all
// proven through the OBS-015 in-memory exporters
// (internal/platform/telemetry/testexport), not a live backend.
func TestTodo_OBS_023_Integration(t *testing.T) {
	at := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)
	ctx := fixedObsTraceContext(context.Background())

	spanRec := otelTestexport.NewSpanRecorder()
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource:        obsTestResource(t),
		Evaluator:       obsTestEvaluator(t),
		ShutdownTimeout: 5 * time.Second,
		Trace:           hcmotel.TraceConfig{Exporter: spanRec},
		Metric:          hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
	})
	if err != nil {
		t.Fatalf("hcmotel.NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	logRec := testexport.NewLogRecorder()
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-obs023"), logging.WithClock(func() time.Time { return at })))
	instrumentation := platformexecution.NewOTelInstrumentation(provider, logger, func() time.Time { return at })

	fx := runPromotionWithTelemetry(t, ctx, "obs023-int", at, instrumentation, execute.NoopExecutionEvidence{})
	// This Provider's own default trace pipeline batches spans; a real
	// backend would flush on its own timer, but a deterministic test flushes
	// explicitly before reading spanRec.
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}

	var nodeSpans, advanceSpans, terminalSpans int
	for _, s := range spanRec.Spans() {
		switch s.Name() {
		case "hcmnext.workflow.node":
			nodeSpans++
		case "hcmnext.workflow.advance":
			advanceSpans++
		case "hcmnext.workflow.terminal":
			terminalSpans++
		}
		if got := s.SpanContext().TraceID(); got != fixedObsTraceID {
			t.Errorf("span %s trace id = %s, want the fixed ambient trace id %s", s.Name(), got, fixedObsTraceID)
		}
	}
	if nodeSpans == 0 {
		t.Error("no node span recorded (want at least the TRANSFORM/END steps)")
	}
	if advanceSpans == 0 {
		t.Error("no advance span recorded")
	}
	if terminalSpans != 1 {
		t.Errorf("terminal spans = %d, want exactly 1", terminalSpans)
	}

	advanceLines := logRec.LinesNamed("workflow.advance")
	if len(advanceLines) == 0 {
		t.Error("no workflow.advance log line recorded")
	}
	terminalLines := logRec.LinesNamed("workflow.terminal.write")
	if len(terminalLines) != 1 {
		t.Fatalf("workflow.terminal.write log lines = %d, want exactly 1", len(terminalLines))
	}
	fields, ok := terminalLines[0]["attrs"].(map[string]any)
	if !ok {
		t.Fatal("workflow.terminal.write log line carries no attrs")
	}
	if fields["trace_id"] != fixedObsTraceID.String() {
		t.Errorf("terminal log line trace_id = %v, want %s", fields["trace_id"], fixedObsTraceID)
	}
	if _, ok := fields["duration_ms"].(float64); !ok {
		t.Errorf("terminal log line carries no duration_ms: %v", fields)
	}

	// The trace id also reached the durable node_execution rows this run
	// produced (OBS-023 GREEN: "propagates ... into StepRequest and
	// AdvanceRequest.TraceID"), readable back long after the live span tree
	// is gone.
	var traceIDCount int
	if err := fx.db.Conn.QueryRow(context.Background(), `
		SELECT count(*) FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 AND trace_id = $3`,
		fx.tenantID, fx.instanceID, fixedObsTraceID.String()).Scan(&traceIDCount); err != nil {
		t.Fatalf("count node executions by trace_id: %v", err)
	}
	if traceIDCount == 0 {
		t.Error("no node_execution row stamped with the ambient trace id")
	}

	// The ledger correlation helper: an operator (or this harness) joins
	// ledger_event.correlation_id back to the workflow instance's own
	// correlation_id through effects.CorrelationUUID, without a second
	// lookup against workflow_instance.
	sequences := lookupByCorrelation(t, fx.db, fx.tenantID, "corr:obs023-int")
	if len(sequences) != 1 {
		t.Fatalf("lookupByCorrelation(%q) = %v, want exactly the one terminal ledger event", "corr:obs023-int", sequences)
	}
}

// lookupByCorrelation is the harness's own worked example of
// effects.CorrelationUUID: joining ledger_event.correlation_id (a uuid
// column) back to a workflow instance's own free-text correlation id (the
// same string runtime.StartRequest.CorrelationID and
// workflow_instance.correlation_id carry) without decoding the ledger
// event's payload first.
func lookupByCorrelation(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, correlationID string) []int64 {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT sequence FROM ledger_event
		WHERE tenant_id = $1 AND correlation_id = $2
		ORDER BY sequence`, tenantID, effects.CorrelationUUID(correlationID))
	if err != nil {
		t.Fatalf("lookupByCorrelation: query: %v", err)
	}
	defer rows.Close()
	var sequences []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("lookupByCorrelation: scan: %v", err)
		}
		sequences = append(sequences, seq)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("lookupByCorrelation: rows: %v", err)
	}
	return sequences
}

// TestTodo_OBS_024_Integration proves OBS-024's GREEN clause against a real
// database and the driver's own three evidence kinds: the completed
// approval is recorded as APPROVAL_COMPLETED, the completed task as
// TASK_SUBMITTED, and the governed terminal write as TERMINAL_WRITTEN, all
// through the same capability evidence sink mechanism CAP-002's gateway
// uses (internal/platform/execution.NewCapabilityEvidenceAdapter over an
// internal/intent/app.MemoryEvidenceSink), and each call's own
// Result.EvidenceIDs names the id(s) it produced.
func TestTodo_OBS_024_Integration(t *testing.T) {
	at := time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)
	ctx := context.Background()

	sink := app.NewMemoryEvidenceSink()
	evidence := platformexecution.NewCapabilityEvidenceAdapter(sink, func() time.Time { return at })

	fx := runPromotionWithTelemetry(t, ctx, "obs024-int", at, execute.NoopInstrumentation{}, evidence)

	if len(fx.execResult.EvidenceIDs) != 0 {
		t.Errorf("Execute (parks at approval) EvidenceIDs = %v, want none yet", fx.execResult.EvidenceIDs)
	}
	if len(fx.approveToTask.EvidenceIDs) != 1 {
		t.Fatalf("approval->task Resume EvidenceIDs = %v, want exactly 1 (APPROVAL_COMPLETED)", fx.approveToTask.EvidenceIDs)
	}
	if len(fx.taskToDone.EvidenceIDs) != 2 {
		t.Fatalf("task->done Resume EvidenceIDs = %v, want exactly 2 (TASK_SUBMITTED, TERMINAL_WRITTEN)", fx.taskToDone.EvidenceIDs)
	}

	records := sink.Records()
	if len(records) != 3 {
		t.Fatalf("evidence records = %d, want 3 (APPROVAL_COMPLETED, TASK_SUBMITTED, TERMINAL_WRITTEN): %+v", len(records), records)
	}
	wantKinds := []string{execute.EvidenceKindApprovalCompleted, execute.EvidenceKindTaskSubmitted, execute.EvidenceKindTerminalWritten}
	for i, want := range wantKinds {
		if records[i].Decision != want {
			t.Errorf("records[%d].Decision = %q, want %q", i, records[i].Decision, want)
		}
	}
	for i, id := range append(append([]string{}, fx.approveToTask.EvidenceIDs...), fx.taskToDone.EvidenceIDs...) {
		if id != records[i].EvidenceID {
			t.Errorf("evidence id %d = %q, want the sink's own %q", i, id, records[i].EvidenceID)
		}
	}
}
