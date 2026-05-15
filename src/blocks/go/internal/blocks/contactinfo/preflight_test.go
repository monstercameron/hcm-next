package contactinfo

import (
	"encoding/json"
	"testing"

	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

func TestExecutePreflightValidContactInfo(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"currentContactInfo":  currentContactInfoFixture(),
		"proposedContactInfo": proposedContactInfoFixture(),
		"effectiveAt":         "2026-06-01",
		"businessReason":      "relocation",
	})

	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if !output.Valid {
		t.Fatalf("expected valid output, got errors: %#v", output.Errors)
	}

	if output.RiskLevel != blockshared.RiskMedium {
		t.Fatalf("expected medium risk for region change, got %s", output.RiskLevel)
	}

	if output.RequiresEvidence || !output.RequiresApproval {
		t.Fatalf("expected no evidence and approval requirement")
	}

	if output.RouteKey != executor.RouteKeyApprovalWithWarnings {
		t.Fatalf("expected approval-with-warnings route key, got %s", output.RouteKey)
	}

	if len(output.Facts) == 0 {
		t.Fatalf("expected generic facts in output")
	}

	if len(output.ValidationErrors) != 0 {
		t.Fatalf("expected no generic validation errors, got %#v", output.ValidationErrors)
	}
}

func TestExecutePreflightRejectsUnchangedContactInfo(t *testing.T) {
	currentContactInfo := currentContactInfoFixture()
	output := executeInvalidPreflight(t, map[string]any{
		"currentContactInfo":  currentContactInfo,
		"proposedContactInfo": currentContactInfo,
		"effectiveAt":         "2026-06-01",
		"businessReason":      "employee_self_service",
	})

	assertValidationCode(t, output.Errors, "contact_info.unchanged")
}

func TestExecutePreflightRejectsInvalidContactInfo(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentContactInfo": currentContactInfoFixture(),
		"proposedContactInfo": map[string]any{
			"personalEmail": "not-an-email",
			"mobilePhone":   "555",
			"homeAddress": map[string]any{
				"line1":      "",
				"line2":      nil,
				"city":       "New York",
				"region":     "NY",
				"postalCode": "10001",
				"country":    "USA",
			},
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "employee_self_service",
	})

	assertValidationCode(t, output.Errors, "contact_info.personal_email_invalid")
	assertValidationCode(t, output.Errors, "contact_info.mobile_phone_invalid")
	assertValidationCode(t, output.Errors, "contact_info.address_required")
	assertValidationCode(t, output.Errors, "contact_info.country_invalid")
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

func currentContactInfoFixture() map[string]any {
	return map[string]any{
		"personalEmail": "jane.personal@example.com",
		"mobilePhone":   "+15550001111",
		"homeAddress": map[string]any{
			"line1":      "100 Market St",
			"line2":      nil,
			"city":       "San Francisco",
			"region":     "CA",
			"postalCode": "94105",
			"country":    "US",
		},
	}
}

func proposedContactInfoFixture() map[string]any {
	return map[string]any{
		"personalEmail": "jane.rivera.personal@example.com",
		"mobilePhone":   "+15559998888",
		"homeAddress": map[string]any{
			"line1":      "200 Park Ave",
			"line2":      "Apt 8",
			"city":       "New York",
			"region":     "NY",
			"postalCode": "10017",
			"country":    "US",
		},
	}
}
