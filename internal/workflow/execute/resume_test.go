package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
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

func resumeFixture() ResumeRequest {
	tenantID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	instanceID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	workItemID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	proposalDigest := "sha256:" + string(make([]byte, 64))
	// NUL is not a valid semantic-key character for production data, but a
	// WorkItem only requires ProposalRef to be present. Use an ordinary digest.
	proposalDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
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
	return ResumeRequest{
		Start: runtime.StartRequest{
			TenantID: tenantID, CellID: "cell-1", StartIdempotencyKey: "start-1",
			Resolver: staticResolver{selection}, Versions: staticVersions{record},
			Proposal: runtime.ProposalBinding{}, CorrelationID: "corr-1",
			BusinessSubjectRefs: []string{"employment:1"},
		},
		InstanceID: instanceID, ExpectedInstanceVersion: 3, RecordedAt: at,
		WorkItem: workitem.WorkItem{
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
		},
		Outcome: frontier.NodeOutcome{
			Outcome:      workflow.Outcome("APPROVED"),
			OutputDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		},
	}
}

func TestResumeAdvancesCompletedWorkItemTypedOutcome(t *testing.T) {
	req := resumeFixture()
	req.Start.Proposal.Revision.MaterialDigest.Digest = req.WorkItem.ProposalRef
	tx := &memoryTx{}
	var got runtime.AdvanceRequest
	driver, err := New(Options{
		DB: oneBeginner{tx}, Steps: noStepRunner{},
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
	if got.Outcome.NodeID != req.WorkItem.NodeID || got.Refs.HumanTaskID != req.WorkItem.WorkItemID.String() {
		t.Fatalf("Advance request did not bind completed human task: %+v", got)
	}
	if got.Outcome.OutputDigest == req.WorkItem.CompletedOutputDigest {
		t.Fatal("fixture did not exercise distinct approval decision and aggregate resolution digests")
	}
}

func TestResumeRefusesMismatchedWorkItemBeforeAdvance(t *testing.T) {
	req := resumeFixture()
	req.Start.Proposal.Revision.MaterialDigest.Digest = req.WorkItem.ProposalRef
	req.WorkItem.WorkflowInstanceID = uuid.New()
	called := false
	driver, err := New(Options{
		DB: oneBeginner{&memoryTx{}}, Steps: noStepRunner{},
		Advance: func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			called = true
			return runtime.AdvanceReceipt{}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := driver.Resume(context.Background(), req); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Resume error = %v, want invalid configuration", err)
	}
	if called {
		t.Fatal("Advance called for mismatched WorkItem")
	}
}

func TestUnsupportedContinuationRollsBackItsAuditInsert(t *testing.T) {
	req := resumeFixture()
	req.Start.Proposal.Revision.MaterialDigest.Digest = req.WorkItem.ProposalRef
	tx := &memoryTx{}
	driver, err := New(Options{
		DB: oneBeginner{tx}, Steps: noStepRunner{},
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
