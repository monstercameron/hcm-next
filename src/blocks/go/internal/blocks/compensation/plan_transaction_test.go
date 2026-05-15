package compensation

import (
	"encoding/json"
	"reflect"
	"testing"

	"hcm-next-executor/internal/executor"
)

func TestExecutePlanTransactionCreatesCompensationWrites(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId":      "chg_123",
		"workerId":             "emp_123",
		"currentCompensation":  compensationFixture(93000),
		"proposedCompensation": compensationFixture(98000),
		"effectiveAt":          "2026-06-01",
		"businessReason":       "retention_adjustment",
	})

	blockResult, executionError := ExecutePlanTransaction(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PlanTransactionOutput)
	if len(output.InternalWrites) != 1 {
		t.Fatalf("expected one internal write, got %d", len(output.InternalWrites))
	}

	internalWrite := output.InternalWrites[0]
	if internalWrite.EventType != compensationUpdatedEventType {
		t.Fatalf("expected EmployeeCompensationUpdated event, got %s", internalWrite.EventType)
	}

	if internalWrite.SubjectType != workerSubjectType || internalWrite.SubjectID != "emp_123" {
		t.Fatalf("expected worker subject emp_123, got %s/%s", internalWrite.SubjectType, internalWrite.SubjectID)
	}

	changedFields := internalWrite.Payload["changedFields"].([]string)
	if !reflect.DeepEqual(changedFields, []string{"compensation.amount"}) {
		t.Fatalf("unexpected changed fields: %#v", changedFields)
	}

	if len(output.ExternalCallRequests) != 1 {
		t.Fatalf("expected one external call request, got %d", len(output.ExternalCallRequests))
	}

	externalCall := output.ExternalCallRequests[0]
	if externalCall.ConnectionID != compensationDecisionConnectionID {
		t.Fatalf("unexpected external connection: %s", externalCall.ConnectionID)
	}

	if externalCall.Operation != submitCompensationChangeOperation {
		t.Fatalf("unexpected external operation: %s", externalCall.Operation)
	}

	if externalCall.Payload["changeRequestId"] != "chg_123" {
		t.Fatalf("expected change request in external payload, got %#v", externalCall.Payload)
	}

	expectedProjectionPatches := []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/compensation",
			Value: CompensationInfo{
				Amount:             98000,
				Currency:           "USD",
				PayFrequency:       "annual",
				BonusTargetPercent: 5,
				EffectiveDate:      "2026-06-01",
			},
		},
	}
	if !reflect.DeepEqual(output.ProjectionPatches, expectedProjectionPatches) {
		t.Fatalf("unexpected projection patches:\nwant: %#v\n got: %#v", expectedProjectionPatches, output.ProjectionPatches)
	}

	if output.RouteKey != executor.RouteKeyTransactionPlanReady {
		t.Fatalf("expected transaction plan route key, got %s", output.RouteKey)
	}

	if output.TransactionPlan == nil {
		t.Fatalf("expected generic transaction plan")
	}

	if len(output.LedgerFacts) != len(output.InternalWrites) {
		t.Fatalf("expected ledger facts to mirror internal writes")
	}

	if len(output.ExternalCalls) != len(output.ExternalCallRequests) {
		t.Fatalf("expected generic external calls to mirror legacy external call requests")
	}

	if len(output.TransactionPlan.ProjectionPatches) != len(output.ProjectionPatches) {
		t.Fatalf("expected transaction plan projection patches to mirror legacy projection patches")
	}
}

func planTransactionRequest(t *testing.T, input map[string]any) executor.ExecutionRequest {
	t.Helper()

	encodedInput, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to encode test input: %v", err)
	}

	return executor.ExecutionRequest{
		TenantID:           "tenant_demo",
		EnvironmentID:      "env_demo",
		WorkflowInstanceID: "wfi_test",
		WorkflowVersionID:  "wfv_test",
		Block: executor.BlockReference{
			Name:    PlanTransactionBlockName,
			Version: BlockVersionV1,
		},
		Input: encodedInput,
		Context: executor.ExecutionContext{
			ActorID:        "actor_hr_admin",
			EffectiveAt:    "2026-06-01",
			Permissions:    map[string]any{},
			CorrelationID:  "corr_test",
			IdempotencyKey: "idem_test",
		},
	}
}
