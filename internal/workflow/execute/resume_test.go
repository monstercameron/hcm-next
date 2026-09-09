package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type staticResolver struct{ selection runtime.WorkflowSelection }

func (r staticResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return r.selection, nil
}

type staticVersions struct{ record version.CompiledVersion }

func (s staticVersions) Put(version.CompiledVersion) error { return nil }
func (s staticVersions) GetByDigest(string) (version.CompiledVersion, bool, error) {
	return s.record, true, nil
}
func (s staticVersions) GetActiveForWorkflow(string) (version.CompiledVersion, bool, error) {
	return s.record, true, nil
}
func (s staticVersions) List(string) ([]version.CompiledVersion, error) {
	return []version.CompiledVersion{s.record}, nil
}

type noStepRunner struct{}

func (noStepRunner) Run(context.Context, StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("unexpected READY step")
}

type memoryTx struct {
	writes     int
	committed  bool
	rolledBack bool
}

func (t *memoryTx) Exec(context.Context, string, ...any) (int64, error) {
	t.writes++
	return 1, nil
}
func (*memoryTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (*memoryTx) QueryRow(context.Context, string, ...any) dbport.Row { return inertRow{} }
func (t *memoryTx) Commit(context.Context) error {
	t.committed = true
	return nil
}
func (t *memoryTx) Rollback(context.Context) error {
	if !t.committed {
		t.writes = 0
		t.rolledBack = true
	}
	return nil
}

type oneBeginner struct{ tx *memoryTx }

func (b oneBeginner) Begin(context.Context) (dbport.Tx, error) { return b.tx, nil }

// fakeWorkItemReader is the [WorkItemReader] test double every resume_test.go
// case uses in place of a real Postgres-backed workitem.Store: it returns
// exactly the durable row it was configured with, so a test can distinguish
// "the stored row is fine" from "the stored row disagrees with the request" by
// setting item's fields directly rather than a struct the request carries
// (WF-RUN-028: Resume never trusts a caller-assembled WorkItem).
type fakeWorkItemReader struct{ item workitem.WorkItem }

func (f fakeWorkItemReader) Load(context.Context, workitem.Executor, uuid.UUID, uuid.UUID) (workitem.WorkItem, error) {
	return f.item, nil
}

// resumeFixture bundles a [ResumeRequest] naming only the stored WorkItem's
// id and expected version, and the WorkItem row itself that a
// [fakeWorkItemReader] built from it hands back to [Driver.Resume].
type resumeFixture struct {
	req  ResumeRequest
	item workitem.WorkItem
}

func newResumeFixture() resumeFixture {
	tenantID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	instanceID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	workItemID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	// NUL is not a valid semantic-key character for production data, but a
	// WorkItem only requires ProposalRef to be present. Use an ordinary digest.
	proposalDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	plan := &workflow.CompiledWorkflow{
		WorkflowID: "promotion.execute", Version: 1,
		Nodes: []workflow.CompiledNode{{ID: "approve", Type: workflow.StepApproval}},
	}
	selection := runtime.WorkflowSelection{
		WorkflowID: "promotion.execute", Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: plan,
	}
	record := version.CompiledVersion{
		WorkflowID: "promotion.execute", SemanticVersion: "1.0.0",
		CompiledPlanDigest: plan.Digest(), Status: version.StatusActive,
	}
	completedAt := at
	item := workitem.WorkItem{
		TenantID: tenantID, WorkItemID: workItemID, ItemVersion: 6,
		Kind: workitem.KindApproval, WorkType: "approval.finance", Status: workitem.StatusCompleted,
		CorrelationID: "corr-1", WorkflowInstanceID: instanceID, NodeID: "approve",
		ApprovalRequirementRef: "approval.finance", ProposalRef: proposalDigest,
		SubjectRefs: []string{"employment:1"}, OwnerKind: workitem.OwnerPrincipal,
		OwnerRef: "principal:approver", PolicyRouteRef: "route.finance",
		Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "org-1",
		DeadlineAt: at.Add(time.Hour), CompletedBy: "principal:approver", CompletedAt: &completedAt,
		CompletedOutputDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CreatedAt:             at.Add(-time.Hour), RecordedAt: at,
	}
	req := ResumeRequest{
		Start: runtime.StartRequest{
			TenantID: tenantID, CellID: "cell-1", StartIdempotencyKey: "start-1",
			Resolver: staticResolver{selection}, Versions: staticVersions{record},
			Proposal: runtime.ProposalBinding{}, CorrelationID: "corr-1",
			BusinessSubjectRefs: []string{"employment:1"},
		},
		InstanceID: instanceID, ExpectedInstanceVersion: 3, RecordedAt: at,
		WorkItemID: workItemID, ExpectedWorkItemVersion: item.ItemVersion,
		Outcome: frontier.NodeOutcome{
			Outcome:      workflow.Outcome("APPROVED"),
			OutputDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		},
	}
	return resumeFixture{req: req, item: item}
}

func TestResumeAdvancesCompletedWorkItemTypedOutcome(t *testing.T) {
	fx := newResumeFixture()
	req := fx.req
	req.Start.Proposal.Revision.MaterialDigest.Digest = fx.item.ProposalRef
	tx := &memoryTx{}
	var got runtime.AdvanceRequest
	driver, err := New(Options{
		DB: oneBeginner{tx}, Steps: noStepRunner{}, Items: fakeWorkItemReader{item: fx.item},
		Advance: func(_ context.Context, _ runtime.Executor, in runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			got = in
			return runtime.AdvanceReceipt{
				TenantID: in.TenantID, InstanceID: in.InstanceID, NodeID: in.Outcome.NodeID,
				NewInstanceVersion: 7, Complete: true,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := driver.Resume(context.Background(), req)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Status != StatusComplete || result.InstanceVersion != 7 || !tx.committed {
		t.Fatalf("result = %+v, transaction committed=%v", result, tx.committed)
	}
	if got.Outcome.NodeID != fx.item.NodeID || got.Refs.HumanTaskID != fx.item.WorkItemID.String() {
		t.Fatalf("Advance request did not bind completed human task: %+v", got)
	}
	if got.Outcome.OutputDigest == fx.item.CompletedOutputDigest {
		t.Fatal("fixture did not exercise distinct approval decision and aggregate resolution digests")
	}
}

// TestResumeRefusesMismatchedWorkItemBeforeAdvance proves WF-RUN-028: a
// WorkItemReader that hands back a row bound to another instance is a
// [ErrWorkItemDrift] refusal, never an advance -- the drift is caught against
// the durable row [Driver.Resume] itself loaded, not against a struct the
// caller assembled.
func TestResumeRefusesMismatchedWorkItemBeforeAdvance(t *testing.T) {
	fx := newResumeFixture()
	req := fx.req
	req.Start.Proposal.Revision.MaterialDigest.Digest = fx.item.ProposalRef
	fx.item.WorkflowInstanceID = uuid.New()
	called := false
	driver, err := New(Options{
		DB: oneBeginner{&memoryTx{}}, Steps: noStepRunner{}, Items: fakeWorkItemReader{item: fx.item},
		Advance: func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			called = true
			return runtime.AdvanceReceipt{}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := driver.Resume(context.Background(), req); !errors.Is(err, ErrWorkItemDrift) {
		t.Fatalf("Resume error = %v, want work item drift", err)
	}
	if called {
		t.Fatal("Advance called for mismatched WorkItem")
	}
}

func TestUnsupportedContinuationRollsBackItsAuditInsert(t *testing.T) {
	fx := newResumeFixture()
	req := fx.req
	req.Start.Proposal.Revision.MaterialDigest.Digest = fx.item.ProposalRef
	tx := &memoryTx{}
	driver, err := New(Options{
		DB: oneBeginner{tx}, Steps: noStepRunner{}, Items: fakeWorkItemReader{item: fx.item},
		Advance: func(ctx context.Context, ex runtime.Executor, in runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			return runtime.AdvanceReceipt{}, in.Sink.RequireTimer(ctx, ex, runtime.ContinuationRecord{
				TenantID: in.TenantID, InstanceID: in.InstanceID, SourceNodeID: in.Outcome.NodeID,
				SourceAttempt: in.Attempt, TargetNodeID: "wait", Kind: frontier.IntentTimerRequired,
				RecordedAt: in.RecordedAt,
			})
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := driver.Resume(context.Background(), req); !errors.Is(err, ErrUnsupportedContinuation) {
		t.Fatalf("Resume error = %v, want unsupported continuation", err)
	}
	if !tx.rolledBack || tx.committed || tx.writes != 0 {
		t.Fatalf("transaction rolledBack=%v committed=%v retained writes=%d", tx.rolledBack, tx.committed, tx.writes)
	}
}
