package legalname

import (
	"encoding/json"
	"testing"

	"hcm-next-executor/internal/executor"
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
			"first": "Jane",
			"last":  "Rivera",
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

	if len(blockResult.ExternalCallRequests) != 1 {
		t.Fatalf("expected top-level external call request for Node outbox creation")
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
