package contactinfo

import (
	"encoding/json"
	"reflect"
	"testing"

	"hcm-next-executor/internal/executor"
)

func TestExecutePlanTransactionCreatesContactInfoWrites(t *testing.T) {
	request := planTransactionRequest(t, map[string]any{
		"changeRequestId":     "chg_123",
		"workerId":            "emp_123",
		"currentContactInfo":  currentContactInfoFixture(),
		"proposedContactInfo": proposedContactInfoFixture(),
		"effectiveAt":         "2026-06-01",
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
	if internalWrite.EventType != contactInfoUpdatedEventType {
		t.Fatalf("expected EmployeeContactInfoUpdated event, got %s", internalWrite.EventType)
	}

	if internalWrite.SubjectType != workerSubjectType || internalWrite.SubjectID != "emp_123" {
		t.Fatalf("expected worker subject emp_123, got %s/%s", internalWrite.SubjectType, internalWrite.SubjectID)
	}

	changedFields := internalWrite.Payload["changedFields"].([]string)
	if !reflect.DeepEqual(changedFields, []string{"contact.personalEmail", "contact.mobilePhone", "contact.homeAddress"}) {
		t.Fatalf("unexpected changed fields: %#v", changedFields)
	}

	if len(output.ExternalCallRequests) != 1 {
		t.Fatalf("expected one external call request, got %d", len(output.ExternalCallRequests))
	}

	if output.ExternalCallRequests[0].Operation != updateContactInfoOperation {
		t.Fatalf("unexpected external operation: %s", output.ExternalCallRequests[0].Operation)
	}

	expectedPersonalEmail := "jane.rivera.personal@example.com"
	expectedMobilePhone := "+15559998888"
	expectedLine2 := "Apt 8"
	expectedHomeAddress := PostalAddress{
		Line1:      "200 Park Ave",
		Line2:      &expectedLine2,
		City:       "New York",
		Region:     "NY",
		PostalCode: "10017",
		Country:    "US",
	}
	expectedProjectionPatches := []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/contact/personalEmail",
			Value:      &expectedPersonalEmail,
		},
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/contact/mobilePhone",
			Value:      &expectedMobilePhone,
		},
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/contact/homeAddress",
			Value:      expectedHomeAddress,
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
			EffectiveAt:    "2026-05-15",
			Permissions:    map[string]any{},
			CorrelationID:  "corr_test",
			IdempotencyKey: "idem_test",
		},
	}
}
