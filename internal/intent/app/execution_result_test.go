package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type resolvedOutcomeSpy struct {
	Store
	calls int
}

func (s *resolvedOutcomeSpy) BindOutcome(context.Context, OutcomeBinding) error {
	s.calls++
	return nil
}

func validResolvedExecutionResult() ExecutionResult {
	return ExecutionResult{
		Status: ExecutionResultResolved, InstanceID: uuid.MustParse("4bca79c1-c393-493a-a798-f01c48c0e28f").String(), InstanceVersion: 9,
		ResolvedStart: &ResolvedStartState{
			RuntimeStatus: runtime.InstanceRunning, CurrentNodeIDs: []string{"approve"},
			WorkflowID: "workflow.promotion", WorkflowVersion: 4,
			CompiledPlanDigest: "sha256:plan", SemanticVersion: "4.2.0",
		},
	}
}

func TestResolvedExecutionResultProjectsTypedCurrentStateWithoutTerminalOutcome(t *testing.T) {
	result := validResolvedExecutionResult()
	if err := result.validate(); err != nil {
		t.Fatal(err)
	}
	receipt := executionReceiptProto(result)
	if receipt.Status != intentsv1.ExecutionReceiptStatus_EXECUTION_RECEIPT_STATUS_RESOLVED || receipt.InstanceId != result.InstanceID || receipt.InstanceVersion != 9 || receipt.ResolvedStart == nil {
		t.Fatalf("resolved receipt = %+v", receipt)
	}
	if receipt.ResolvedStart.RuntimeStatus != "RUNNING" || receipt.ResolvedStart.WorkflowId != "workflow.promotion" || receipt.ResolvedStart.WorkflowVersion != 4 || receipt.ResolvedStart.CompiledPlanDigest != "sha256:plan" || receipt.ResolvedStart.SemanticVersion != "4.2.0" || len(receipt.ResolvedStart.CurrentNodeIds) != 1 {
		t.Fatalf("resolved state = %+v", receipt.ResolvedStart)
	}
	result.ResolvedStart.CurrentNodeIDs[0] = "mutated"
	if receipt.ResolvedStart.CurrentNodeIds[0] != "approve" {
		t.Fatal("wire current frontier aliases application result")
	}
	if _, found, err := OutcomeReceiptFromExecution(intent.Instance{IntentID: "intent-1"}, result, time.Unix(1, 0)); err != nil || found {
		t.Fatalf("resolved proof terminal outcome found=%v err=%v", found, err)
	}
	if got := receiptDigestFor(result); got != "execution:"+result.InstanceID+":RESOLVED:9" {
		t.Fatalf("resolved digest = %q", got)
	}
}

func TestConsumeExecutionResultDoesNotBindResolvedStartAsTerminal(t *testing.T) {
	store := &resolvedOutcomeSpy{}
	svc := &IntentService{store: store, clock: func() values.Instant { return values.NewInstant(time.Unix(1, 0)) }}
	if err := svc.consumeExecutionResult(context.Background(), intent.Instance{IntentID: "intent-1"}, intent.Definition{}, IntentRecord{}, validResolvedExecutionResult()); err != nil {
		t.Fatal(err)
	}
	if store.calls != 0 {
		t.Fatalf("BindOutcome calls = %d, want zero", store.calls)
	}
}

func TestExecutionResultValidationRejectsUnknownAndMalformedResolvedStates(t *testing.T) {
	valid := validResolvedExecutionResult()
	tests := map[string]func(*ExecutionResult){
		"unknown status":       func(r *ExecutionResult) { r.Status = "SURPRISE" },
		"junk instance id":     func(r *ExecutionResult) { r.InstanceID = "not-a-uuid" },
		"nil instance id":      func(r *ExecutionResult) { r.InstanceID = uuid.Nil.String() },
		"missing proof":        func(r *ExecutionResult) { r.ResolvedStart = nil },
		"zero version":         func(r *ExecutionResult) { r.InstanceVersion = 0 },
		"unknown runtime":      func(r *ExecutionResult) { r.ResolvedStart.RuntimeStatus = "UNKNOWN" },
		"empty node":           func(r *ExecutionResult) { r.ResolvedStart.CurrentNodeIDs = []string{""} },
		"duplicate node":       func(r *ExecutionResult) { r.ResolvedStart.CurrentNodeIDs = []string{"approve", "approve"} },
		"missing plan pin":     func(r *ExecutionResult) { r.ResolvedStart.CompiledPlanDigest = "" },
		"partial lifecycle":    func(r *ExecutionResult) { r.ResolvedStart.Lifecycle.RequestState = "APPROVED" },
		"terminal dimensions":  func(r *ExecutionResult) { r.TerminalCode = "COMPLETED" },
		"parked contradiction": func(r *ExecutionResult) { r.Parked = true },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			got := valid
			state := *valid.ResolvedStart
			state.CurrentNodeIDs = append([]string(nil), valid.ResolvedStart.CurrentNodeIDs...)
			got.ResolvedStart = &state
			mutate(&got)
			if err := got.validate(); err == nil {
				t.Fatal("malformed result accepted")
			}
			if _, found, err := OutcomeReceiptFromExecution(intent.Instance{}, got, time.Time{}); err == nil || found {
				t.Fatalf("malformed result outcome found=%v err=%v", found, err)
			}
		})
	}
}

func TestLegacyExecutionReceiptStatusAndDigestRemainStable(t *testing.T) {
	parked := ExecutionResult{Parked: true, InstanceID: "legacy"}
	complete := ExecutionResult{InstanceID: "legacy"}
	if executionReceiptProto(parked).Status != intentsv1.ExecutionReceiptStatus_EXECUTION_RECEIPT_STATUS_PARKED || receiptDigestFor(parked) != "execution:legacy:PARKED" {
		t.Fatal("legacy parked projection changed")
	}
	if executionReceiptProto(complete).Status != intentsv1.ExecutionReceiptStatus_EXECUTION_RECEIPT_STATUS_COMPLETE || receiptDigestFor(complete) != "execution:legacy:COMPLETE" {
		t.Fatal("legacy complete projection changed")
	}
}
