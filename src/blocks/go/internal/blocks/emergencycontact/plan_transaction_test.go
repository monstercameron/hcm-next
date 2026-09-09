package emergencycontact

import (
	"encoding/json"
	"reflect"
	"testing"

	"human-capital-management-suite-executor/internal/executor"
)

func TestExecutePlanTransactionCreatesEmergencyContactWrites(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId": "chg_123",
		"workerId":        "emp_123",
		"currentEmergencyContacts": []map[string]any{
			{
				"contactId":    "ec_001",
				"name":         "Alex Doe",
				"relationship": "spouse",
				"phone":        "+15551234567",
				"email":        "alex.doe@example.com",
				"priority":     1,
			},
		},
		"proposedEmergencyContact": map[string]any{
			"contactId":    "ec_001",
			"name":         "Alex Doe",
			"relationship": "spouse",
			"phone":        "+15557654321",
			"email":        "alex.doe@example.com",
			"priority":     1,
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

	if output.InternalWrites[0].EventType != emergencyContactUpdatedEventType {
		t.Fatalf("unexpected internal write event type: %s", output.InternalWrites[0].EventType)
	}

	if len(output.ExternalCallRequests) != 1 {
		t.Fatalf("expected one external call request, got %d", len(output.ExternalCallRequests))
	}

	if output.ExternalCallRequests[0].Operation != updateEmergencyContactsOperation {
		t.Fatalf("unexpected external operation: %s", output.ExternalCallRequests[0].Operation)
	}

	expectedEmail := "alex.doe@example.com"
	expectedEmergencyContacts := []EmergencyContact{
		{
			ContactID:    "ec_001",
			Name:         "Alex Doe",
			Relationship: "spouse",
			Phone:        "+15557654321",
			Email:        &expectedEmail,
			Priority:     1,
		},
	}
	expectedProjectionPatches := []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/emergencyContacts",
			Value:      expectedEmergencyContacts,
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
