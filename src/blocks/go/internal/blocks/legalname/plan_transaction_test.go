package legalname

import (
	"encoding/json"
	"reflect"
	"testing"

	"human-capital-management-suite-executor/internal/executor"
)

func TestExecutePlanTransactionOutputShape(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId": "cr_123",
		"workerId":        "emp_123",
		"personId":        "person_123",
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first":  "Jane",
			"middle": "A.",
			"last":   "Rivera",
		},
		"effectiveAt": "2026-06-01",
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
	if internalWrite.EventType != personLegalNameChangedEventType {
		t.Fatalf("expected PersonLegalNameChanged event, got %s", internalWrite.EventType)
	}

	if internalWrite.SubjectType != workerSubjectType || internalWrite.SubjectID != "emp_123" {
		t.Fatalf("expected worker subject emp_123, got %s/%s", internalWrite.SubjectType, internalWrite.SubjectID)
	}

	if internalWrite.Payload["personId"] != "person_123" {
		t.Fatalf("expected person_123 payload, got %#v", internalWrite.Payload)
	}

	if len(output.ExternalCallRequests) != 1 {
		t.Fatalf("expected one external call request, got %d", len(output.ExternalCallRequests))
	}

	externalCallRequest := output.ExternalCallRequests[0]
	if externalCallRequest.ConnectionID != fakeHRISConnectionID {
		t.Fatalf("expected fake_hris connection, got %s", externalCallRequest.ConnectionID)
	}

	if externalCallRequest.Operation != updateLegalNameOperation {
		t.Fatalf("expected updateLegalName operation, got %s", externalCallRequest.Operation)
	}

	if externalCallRequest.IdempotencyKey != "fake_hris_legal_name_cr_123" {
		t.Fatalf("unexpected idempotency key: %s", externalCallRequest.IdempotencyKey)
	}

	expectedMiddleName := "A."
	expectedProjectionPatches := []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/person/legalName",
			Value: LegalName{
				First:  "Jane",
				Middle: &expectedMiddleName,
				Last:   "Rivera",
			},
		},
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/person/displayName",
			Value:      "Jane A. Rivera",
		},
	}
	if !reflect.DeepEqual(output.ProjectionPatches, expectedProjectionPatches) {
		t.Fatalf("unexpected projection patches:\nwant: %#v\n got: %#v", expectedProjectionPatches, output.ProjectionPatches)
	}

	if len(blockResult.ExternalCallRequests) != 1 {
		t.Fatalf("expected top-level external call request for Node outbox creation")
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
		ChangeRequestID:    "cr_123",
		WorkflowInstanceID: "wfi_test",
		WorkflowVersionID:  "wfv_test",
		Block: executor.BlockReference{
			Name:    PlanTransactionBlockName,
			Version: BlockVersionV1,
		},
		Input: encodedInput,
		Context: executor.ExecutionContext{
			ActorID:        "actor_hr_admin",
			EffectiveAt:    "2026-05-15",
			Permissions:    map[string]any{},
			CorrelationID:  "corr_test",
			IdempotencyKey: "idem_test",
		},
	}
}
