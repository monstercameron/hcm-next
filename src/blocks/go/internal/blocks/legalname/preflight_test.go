package legalname

import (
	"encoding/json"
	"testing"

	"hcm-next-executor/internal/executor"
)

func TestExecutePreflightValidInput(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"currentLegalName": map[string]any{
			"first":  "Jane",
			"middle": nil,
			"last":   "Doe",
		},
		"proposedLegalName": map[string]any{
			"first":  "Jane",
			"middle": nil,
			"last":   "Rivera",
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "legal_name_change",
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

	if !output.RequiresEvidence || !output.RequiresApproval {
		t.Fatalf("expected evidence and approval requirements to be true")
	}
}

func TestExecutePreflightMissingFirstName(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "",
			"last":  "Rivera",
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "legal_name_change",
	})

	assertValidationCode(t, output.Errors, "legal_name.first_required")
}

func TestExecutePreflightMissingLastName(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "Jane",
			"last":  "",
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "legal_name_change",
	})

	assertValidationCode(t, output.Errors, "legal_name.last_required")
}

func TestExecutePreflightUnchangedName(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentLegalName": map[string]any{
			"first":  "Jane",
			"middle": nil,
			"last":   "Doe",
		},
		"proposedLegalName": map[string]any{
			"first":  "Jane",
			"middle": nil,
			"last":   "Doe",
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "legal_name_change",
	})

	assertValidationCode(t, output.Errors, "legal_name.unchanged")
}

func TestExecutePreflightMissingBusinessReason(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "Jane",
			"last":  "Rivera",
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "",
	})

	assertValidationCode(t, output.Errors, "legal_name.business_reason_required")
}

func TestExecutePreflightMissingEffectiveDate(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "Jane",
			"last":  "Rivera",
		},
		"effectiveAt":    "",
		"businessReason": "legal_name_change",
	})

	assertValidationCode(t, output.Errors, "legal_name.effective_at_required")
}

func TestExecutePreflightEffectiveDateTooFarInPast(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "Jane",
			"last":  "Rivera",
		},
		"effectiveAt":    "2025-11-15",
		"businessReason": "legal_name_change",
	})

	assertValidationCode(t, output.Errors, "legal_name.effective_at_too_far_in_past")
}

func TestExecutePreflightReturnsAllValidationErrors(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "",
			"last":  "",
		},
		"effectiveAt":    "",
		"businessReason": "",
	})

	expectedCodes := []string{
		"legal_name.first_required",
		"legal_name.last_required",
		"legal_name.business_reason_required",
		"legal_name.effective_at_required",
	}

	for _, expectedCode := range expectedCodes {
		assertValidationCode(t, output.Errors, expectedCode)
	}
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
