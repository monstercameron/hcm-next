package compensation

import (
	"encoding/json"
	"testing"

	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

func TestExecutePreflightValidCompensationChange(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"currentCompensation":  compensationFixture(93000),
		"proposedCompensation": compensationFixture(98000),
		"currentJob":           map[string]any{"jobCode": "OPS-HRBP2", "level": "P2"},
		"currentOrganization":  map[string]any{"payZone": "US-EAST"},
		"effectiveAt":          "2026-06-01",
		"businessReason":       "retention_adjustment",
	})

	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if !output.Valid {
		t.Fatalf("expected valid output, got errors: %#v", output.Errors)
	}

	if output.RiskLevel != blockshared.RiskLow {
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

func TestExecutePreflightRejectsNonRaise(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"currentCompensation":  compensationFixture(93000),
		"proposedCompensation": compensationFixture(93000),
		"currentJob":           map[string]any{"jobCode": "OPS-HRBP2", "level": "P2"},
		"currentOrganization":  map[string]any{"payZone": "US-EAST"},
		"effectiveAt":          "2026-06-01",
		"businessReason":       "retention_adjustment",
	})

	assertValidationCode(t, output.Errors, "compensation.not_a_raise")
}

func TestExecutePreflightWarnsOnLargeRaise(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"currentCompensation":  compensationFixture(93000),
		"proposedCompensation": compensationFixture(105000),
		"currentJob":           map[string]any{"jobCode": "OPS-HRBP2", "level": "P2"},
		"currentOrganization":  map[string]any{"payZone": "US-EAST"},
		"effectiveAt":          "2026-06-01",
		"businessReason":       "retention_adjustment",
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
		t.Fatalf("expected medium risk, got %s", output.RiskLevel)
	}

	if output.RouteKey != executor.RouteKeyApprovalWithWarnings {
		t.Fatalf("expected approval-with-warnings route key, got %s", output.RouteKey)
	}

	assertValidationCode(t, output.Warnings, "compensation.increase_over_ten_percent")
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
			ActorID:        "actor_hr_admin",
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

func compensationFixture(amount float64) map[string]any {
	return map[string]any{
		"amount":             amount,
		"currency":           "USD",
		"payFrequency":       "annual",
		"bonusTargetPercent": 5,
		"effectiveDate":      "2026-06-01",
	}
}
