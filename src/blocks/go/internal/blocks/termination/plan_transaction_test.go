package termination

import (
	"encoding/json"
	"testing"

	"hcm-next-executor/internal/executor"
)

func TestExecutePlanTransactionVoluntaryTermination(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId": "cr_001",
		"workerId":        "emp_123",
		"effectiveAt":     "2026-06-30",
		"terminationType": "voluntary",
		"cobraWindowDays": 60,
		"finalPayDate":    "2026-06-30",
	})

	blockResult, executionError := ExecutePlanTransaction(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PlanTransactionOutput)

	if len(output.InternalWrites) != 1 {
		t.Fatalf("expected 1 internal write, got %d", len(output.InternalWrites))
	}

	if output.InternalWrites[0].EventType != terminationExecutedEventType {
		t.Fatalf("expected event type %s, got %s", terminationExecutedEventType, output.InternalWrites[0].EventType)
	}

	if len(output.ExternalCallRequests) != 2 {
		t.Fatalf("expected 2 external call requests (payroll + benefits), got %d", len(output.ExternalCallRequests))
	}

	payrollFound := false
	benefitsFound := false
	for _, ecr := range output.ExternalCallRequests {
		if ecr.ConnectionID == payrollConnectionID && ecr.Operation == processFinalPayOperation {
			payrollFound = true
		}
		if ecr.ConnectionID == benefitsConnectionID && ecr.Operation == triggerCobraOperation {
			benefitsFound = true
		}
	}

	if !payrollFound {
		t.Fatalf("expected payroll external call request not found")
	}
	if !benefitsFound {
		t.Fatalf("expected benefits COBRA external call request not found")
	}

	if len(output.ProjectionPatches) != 1 {
		t.Fatalf("expected 1 projection patch, got %d", len(output.ProjectionPatches))
	}

	if output.ProjectionPatches[0].Path != "/employment/status" {
		t.Fatalf("expected /employment/status patch, got %s", output.ProjectionPatches[0].Path)
	}

	if output.ProjectionPatches[0].Value != "terminated" {
		t.Fatalf("expected terminated value, got %v", output.ProjectionPatches[0].Value)
	}

	if output.RouteKey != executor.RouteKeyTransactionPlanReady {
		t.Fatalf("expected planned route key, got %s", output.RouteKey)
	}
}

func TestExecutePlanTransactionDefaultsCobraWindowDays(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId": "cr_001",
		"workerId":        "emp_123",
		"effectiveAt":     "2026-06-30",
		"terminationType": "involuntary",
		"cobraWindowDays": 0,
		"finalPayDate":    "2026-06-30",
	})

	blockResult, executionError := ExecutePlanTransaction(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PlanTransactionOutput)
	if len(output.InternalWrites) != 1 {
		t.Fatalf("expected 1 internal write even with zero cobraWindowDays")
	}

	payload := output.InternalWrites[0].Payload
	cobraWindow, ok := payload["cobraWindowDays"]
	if !ok {
		t.Fatalf("expected cobraWindowDays in internal write payload")
	}

	if cobraWindow != 60 {
		t.Fatalf("expected default 60 COBRA window days, got %v", cobraWindow)
	}
}

func TestExecutePlanTransactionRejectsMissingChangeRequestId(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId": "",
		"workerId":        "emp_123",
		"effectiveAt":     "2026-06-30",
		"terminationType": "voluntary",
		"cobraWindowDays": 60,
		"finalPayDate":    "2026-06-30",
	})

	_, executionError := ExecutePlanTransaction(request)
	if executionError == nil {
		t.Fatalf("expected execution error for missing changeRequestId")
	}
}

func TestExecutePlanTransactionRejectsMissingWorkerId(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId": "cr_001",
		"workerId":        "",
		"effectiveAt":     "2026-06-30",
		"terminationType": "voluntary",
		"cobraWindowDays": 60,
		"finalPayDate":    "2026-06-30",
	})

	_, executionError := ExecutePlanTransaction(request)
	if executionError == nil {
		t.Fatalf("expected execution error for missing workerId")
	}
}

func TestExecutePlanTransactionIdempotencyKeysAreUnique(t *testing.T) {
	request1 := planTransactionRequest(t, map[string]any{
		"changeRequestId": "cr_001",
		"workerId":        "emp_123",
		"effectiveAt":     "2026-06-30",
		"terminationType": "voluntary",
		"cobraWindowDays": 60,
		"finalPayDate":    "2026-06-30",
	})
	request2 := planTransactionRequest(t, map[string]any{
		"changeRequestId": "cr_002",
		"workerId":        "emp_456",
		"effectiveAt":     "2026-07-31",
		"terminationType": "retirement",
		"cobraWindowDays": 60,
		"finalPayDate":    "2026-07-31",
	})

	result1, err1 := ExecutePlanTransaction(request1)
	result2, err2 := ExecutePlanTransaction(request2)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}

	output1 := result1.Output.(PlanTransactionOutput)
	output2 := result2.Output.(PlanTransactionOutput)

	keys1 := make(map[string]bool)
	for _, ecr := range output1.ExternalCallRequests {
		keys1[ecr.IdempotencyKey] = true
	}

	for _, ecr := range output2.ExternalCallRequests {
		if keys1[ecr.IdempotencyKey] {
			t.Fatalf("idempotency key collision between different change requests: %s", ecr.IdempotencyKey)
		}
	}
}

func planTransactionRequest(t *testing.T, inputFields map[string]any) executor.ExecutionRequest {
	t.Helper()

	inputJSON, err := json.Marshal(inputFields)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}

	return executor.ExecutionRequest{
		Input: inputJSON,
		Context: executor.ExecutionContext{
			EffectiveAt:    "2026-05-15",
			IdempotencyKey: "test-idempotency-key",
		},
	}
}
