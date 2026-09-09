package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
)

type repairEffectDouble struct {
	calls []RepairEffectRequest
	err   error
}

func (d *repairEffectDouble) ExecuteRepairEffect(_ context.Context, req RepairEffectRequest) (RepairEffectResult, error) {
	d.calls = append(d.calls, req)
	if d.err != nil {
		return RepairEffectResult{}, d.err
	}
	return RepairEffectResult{EffectKey: req.Step.EffectKey, EffectRef: req.Step.EffectRef, Accepted: true, ResultRef: "effect-result:1"}, nil
}

type repairObservationDouble struct{}

func (repairObservationDouble) ObserveRepair(context.Context, operationrepair.RepairPlan, RepairEffectResult) (RepairObservation, error) {
	return RepairObservation{Observed: true, Complete: true, Digest: "sha256:observed-repair", State: "IAM_EXPECTED"}, nil
}

type repairReconciliationDouble struct{ decision reconcile.CompletionDecision }

func (d repairReconciliationDouble) ReconcileRepair(context.Context, RepairReconciliationRequest) (reconcile.CompletionDecision, error) {
	return d.decision, nil
}

type repairApprovalDouble struct{ calls int }

func (d *repairApprovalDouble) ApproveRepair(context.Context, RepairApprovalRequest) (RepairApprovalResult, error) {
	d.calls++
	return RepairApprovalResult{Approved: true, Digest: "approval:repair-1"}, nil
}

func executeRepairPlan() operationrepair.RepairPlan {
	return operationrepair.RepairPlan{
		ID: "repair-promotion-1", Digest: "sha256:plan-1", FindingDigest: "sha256:finding-1",
		ObservationDigest: "sha256:observation-1", AuthorityPolicy: "authority.promotion/1",
		MappingVersion: "mapping.iam/1", CredentialRef: "credential:iam-1", TargetVersion: "iam:worker-1@7",
		OriginalSemanticKey: "promotion:worker-1:proposal-1", FailedEffectKey: "effect:iam-provision",
		Steps: []operationrepair.Step{
			{Ordinal: 1, EffectKey: "effect:unrelated", EffectRef: "operation:unrelated", Target: "iam:worker-1", ExpectedVersion: "iam:worker-1@7", MaxAttempts: 2},
			{Ordinal: 2, EffectKey: "effect:iam-provision", EffectRef: "operation:iam-1", Target: "iam:worker-1", ExpectedVersion: "iam:worker-1@7", MaxAttempts: 2},
		},
	}
}

func executeRepairEvidence() operationrepair.CurrentEvidence {
	return operationrepair.CurrentEvidence{
		PlanDigest: "sha256:plan-1", FindingDigest: "sha256:finding-1", ObservationDigest: "sha256:observation-1",
		AuthorityPolicy: "authority.promotion/1", MappingVersion: "mapping.iam/1", CredentialRef: "credential:iam-1",
		TargetVersion: "iam:worker-1@7", Facts: map[string]string{"worker": "worker-1"},
	}
}

func newRepairExecutor(t *testing.T, effect *repairEffectDouble, approval RepairApprovalPort, decision reconcile.CompletionDecision) *RepairExecutor {
	t.Helper()
	executor, err := NewRepairExecutor(RepairExecutionOptions{
		Admission: operationrepair.NewMemoryStore(), Approval: approval, Effect: effect,
		Observation: repairObservationDouble{}, Reconciliation: repairReconciliationDouble{decision: decision},
	})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func passingRepairDecision() reconcile.CompletionDecision {
	return reconcile.CompletionDecision{Status: reconcile.CompletionPass, Terminal: true, Route: reconcile.RouteConsistent, Reason: "fresh complete observation matches intended and canonical values"}
}

// TestTodo_WF_RUN_016 proves the distinct REPAIR mode lifecycle and its
// one-effect invariant. The parent Promotion semantic identity reaches the
// effect port unchanged, while the repair itself is fenced separately and
// closes only after RECON-002 returns CONSISTENT.
func TestTodo_WF_RUN_016(t *testing.T) {
	effects := &repairEffectDouble{}
	executor := newRepairExecutor(t, effects, nil, passingRepairDecision())
	result, err := executor.Execute(context.Background(), RepairExecutionRequest{Plan: executeRepairPlan(), Current: executeRepairEvidence(), Now: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), Actor: "operator:repair"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != RepairExecutionMode || result.Status != RepairCompleted || !result.Executed || result.ConsistencyState != "CONSISTENT" {
		t.Fatalf("repair result = %+v, want REPAIR/COMPLETED/executed/CONSISTENT", result)
	}
	if len(effects.calls) != 1 {
		t.Fatalf("effect calls = %d, want exactly one failed effect", len(effects.calls))
	}
	call := effects.calls[0]
	if call.Step.EffectKey != "effect:iam-provision" || call.OriginalSemanticKey != "promotion:worker-1:proposal-1" || call.Mode != RepairExecutionMode {
		t.Fatalf("effect request = %+v, want failed effect, original key and REPAIR mode", call)
	}
	if result.Fence.FenceID == "" || result.Fence.FenceID == call.OriginalSemanticKey {
		t.Fatalf("repair fence = %+v, want distinct fenced identity", result.Fence)
	}
	if len(result.Evidence) != 6 || result.Evidence[len(result.Evidence)-1].Stage != "VERIFIED" {
		t.Fatalf("repair evidence = %+v, want six stages ending VERIFIED", result.Evidence)
	}
}

func TestTodo_WF_RUN_016_Race(t *testing.T) {
	effects := &repairEffectDouble{}
	executor := newRepairExecutor(t, effects, nil, passingRepairDecision())
	for i := 0; i < 2; i++ {
		result, err := executor.Execute(context.Background(), RepairExecutionRequest{Plan: executeRepairPlan(), Current: executeRepairEvidence(), Now: time.Date(2026, 9, 5, 12, 0, i, 0, time.UTC)})
		if err != nil || result.Status != RepairCompleted {
			t.Fatalf("run %d result=%+v err=%v", i, result, err)
		}
	}
	if len(effects.calls) != 1 {
		t.Fatalf("effect calls = %d, want one idempotent redrive", len(effects.calls))
	}
}

func TestTodo_WF_RUN_016_Fault(t *testing.T) {
	effects := &repairEffectDouble{err: errors.New("provider unavailable")}
	executor := newRepairExecutor(t, effects, nil, passingRepairDecision())
	result, err := executor.Execute(context.Background(), RepairExecutionRequest{Plan: executeRepairPlan(), Current: executeRepairEvidence(), Now: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)})
	if err == nil || result.Status != RepairFailed || result.Status == RepairCompleted {
		t.Fatalf("failed repair result=%+v err=%v, want FAILED", result, err)
	}
}

func TestTodo_WF_RUN_016_Mutation(t *testing.T) {
	effects := &repairEffectDouble{}
	executor := newRepairExecutor(t, effects, nil, reconcile.CompletionDecision{Status: reconcile.CompletionMismatch, Terminal: true, Route: reconcile.RouteDegraded})
	result, err := executor.Execute(context.Background(), RepairExecutionRequest{Plan: executeRepairPlan(), Current: executeRepairEvidence(), Now: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RepairReconciliationWait || result.Status == RepairCompleted {
		t.Fatalf("unverified repair result=%+v, want RECONCILIATION_REQUIRED", result)
	}
}

func TestTodo_WF_RUN_016_Reapproval(t *testing.T) {
	plan := executeRepairPlan()
	plan.RequiresApproval = true
	plan.ApprovalDigest = "approval:repair-1"
	effects := &repairEffectDouble{}
	approval := &repairApprovalDouble{}
	executor := newRepairExecutor(t, effects, approval, passingRepairDecision())
	result, err := executor.Execute(context.Background(), RepairExecutionRequest{Plan: plan, Current: func() operationrepair.CurrentEvidence {
		evidence := executeRepairEvidence()
		evidence.ApprovalValid = true
		evidence.ApprovalDigest = "approval:repair-1"
		return evidence
	}(), Now: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)})
	if err != nil || result.Status != RepairCompleted || approval.calls != 1 {
		t.Fatalf("approval result=%+v err=%v calls=%d", result, err, approval.calls)
	}
}
