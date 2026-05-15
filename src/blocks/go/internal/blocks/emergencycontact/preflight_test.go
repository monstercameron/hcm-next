package emergencycontact

import (
	"encoding/json"
	"testing"

	"hcm-next-executor/internal/executor"
)

func TestExecutePreflightValidEmergencyContact(t *testing.T) {
	request := preflightRequest(t, map[string]any{
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
		"effectiveAt":    "2026-06-01",
		"businessReason": "employee_self_service",
	})

	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if !output.Valid {
		t.Fatalf("expected valid output, got errors: %#v", output.Errors)
	}

	if output.RiskLevel != preflightRiskLow {
		t.Fatalf("expected low risk, got %s", output.RiskLevel)
	}

	if output.RequiresEvidence || !output.RequiresApproval {
		t.Fatalf("expected no evidence and approval requirement")
	}

	if output.RouteKey != executor.RouteKeyApprovalRequired {
		t.Fatalf("expected approval route key, got %s", output.RouteKey)
	}

	if len(output.Facts) == 0 {
		t.Fatalf("expected generic facts in output")
	}

	if len(output.ValidationErrors) != 0 {
		t.Fatalf("expected no generic validation errors, got %#v", output.ValidationErrors)
	}
}

func TestExecutePreflightRejectsUnchangedEmergencyContact(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
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
			"phone":        "+15551234567",
			"email":        "alex.doe@example.com",
			"priority":     1,
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "employee_self_service",
	})

	assertValidationCode(t, output.Errors, "emergency_contact.unchanged")
}

func TestExecutePreflightRejectsInvalidPhone(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentEmergencyContacts": []map[string]any{},
		"proposedEmergencyContact": map[string]any{
			"contactId":    "ec_002",
			"name":         "Taylor Doe",
			"relationship": "sibling",
			"phone":        "555",
			"email":        nil,
			"priority":     2,
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "employee_self_service",
	})

	assertValidationCode(t, output.Errors, "emergency_contact.phone_invalid")
}

func executeInvalidPreflight(t *testing.T, input map[string]any) PreflightOutput {
	t.Helper()

	request := preflightRequest(t, input)
	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected business validation output, got executor error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if output.Valid {
		t.Fatalf("expected invalid output")
	}

	if output.RouteKey != executor.RouteKeyValidationFailed {
		t.Fatalf("expected validation failure route key, got %s", output.RouteKey)
	}

	return output
}

func preflightRequest(t *testing.T, input map[string]any) executor.ExecutionRequest {
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
			Name:    PreflightBlockName,
			Version: BlockVersionV1,
		},
		Input: encodedInput,
		Context: executor.ExecutionContext{
			ActorID:        "actor_employee_jane",
			EffectiveAt:    "2026-05-15",
			Permissions:    map[string]any{},
			CorrelationID:  "corr_test",
			IdempotencyKey: "idem_test",
		},
	}
}

func assertValidationCode(t *testing.T, validationMessages []ValidationMessage, expectedCode string) {
	t.Helper()

	for _, validationMessage := range validationMessages {
		if validationMessage.Code == expectedCode {
			return
		}
	}

	t.Fatalf("expected validation code %s in %#v", expectedCode, validationMessages)
}
