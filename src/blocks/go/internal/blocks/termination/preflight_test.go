package termination

import (
	"encoding/json"
	"testing"

	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

func TestExecutePreflightValidVoluntaryTermination(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "active",
		"hireDate":                "2020-01-15",
		"effectiveAt":             "2026-06-30",
		"terminationType":         "voluntary",
		"businessReason":          "Employee resignation accepted.",
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
		t.Fatalf("expected low risk for voluntary termination, got %s", output.RiskLevel)
	}

	if !output.RequiresApproval {
		t.Fatalf("expected approval requirement")
	}

	if output.CobraWindowDays != 60 {
		t.Fatalf("expected 60 COBRA window days, got %d", output.CobraWindowDays)
	}

	if output.StatutoryNoticeDays != 0 {
		t.Fatalf("expected 0 statutory notice days for voluntary, got %d", output.StatutoryNoticeDays)
	}

	if output.TenureYears <= 0 {
		t.Fatalf("expected positive tenure years")
	}

	if output.FinalPayDate == "" {
		t.Fatalf("expected a final pay date")
	}

	if output.RouteKey != executor.RouteKeyApprovalRequired {
		t.Fatalf("expected approval_required route key, got %s", output.RouteKey)
	}
}

func TestExecutePreflightInvoluntaryTerminationIsHighRisk(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "active",
		"hireDate":                "2020-01-15",
		"effectiveAt":             "2026-06-30",
		"terminationType":         "involuntary",
		"businessReason":          "Performance improvement plan failed.",
	})

	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if !output.Valid {
		t.Fatalf("expected valid output, got errors: %#v", output.Errors)
	}

	if output.RiskLevel != blockshared.RiskHigh {
		t.Fatalf("expected high risk for involuntary termination, got %s", output.RiskLevel)
	}

	if output.StatutoryNoticeDays != 14 {
		t.Fatalf("expected 14 statutory notice days for involuntary, got %d", output.StatutoryNoticeDays)
	}

	assertWarningCode(t, output.Warnings, "termination.involuntary_legal_review")
}

func TestExecutePreflightRejectsInactiveEmployee(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "terminated",
		"hireDate":                "2020-01-15",
		"effectiveAt":             "2026-06-30",
		"terminationType":         "voluntary",
		"businessReason":          "Resignation accepted.",
	})

	assertValidationCode(t, output.Errors, "termination.employee_not_active")
}

func TestExecutePreflightRejectsInvalidTerminationType(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "active",
		"hireDate":                "2020-01-15",
		"effectiveAt":             "2026-06-30",
		"terminationType":         "fired",
		"businessReason":          "Fired for cause.",
	})

	assertValidationCode(t, output.Errors, "termination.termination_type_invalid")
}

func TestExecutePreflightRejectsMissingBusinessReason(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "active",
		"hireDate":                "2020-01-15",
		"effectiveAt":             "2026-06-30",
		"terminationType":         "voluntary",
		"businessReason":          "",
	})

	assertValidationCode(t, output.Errors, "termination.business_reason_required")
}

func TestExecutePreflightShortTenureWarning(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "active",
		"hireDate":                "2026-01-01",
		"effectiveAt":             "2026-06-30",
		"terminationType":         "voluntary",
		"businessReason":          "Personal reasons.",
	})

	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if !output.Valid {
		t.Fatalf("expected valid despite warnings, got errors: %#v", output.Errors)
	}

	assertWarningCode(t, output.Warnings, "termination.short_tenure")
}

func TestExecutePreflightRejectsEffectiveDateTooFarInPast(t *testing.T) {
	output := executeInvalidPreflight(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "active",
		"hireDate":                "2020-01-15",
		"effectiveAt":             "2025-01-01",
		"terminationType":         "voluntary",
		"businessReason":          "Resignation.",
	})

	assertValidationCode(t, output.Errors, "termination.effective_at_too_far_in_past")
}

func TestExecutePreflightRetirementHasMediumRisk(t *testing.T) {
	request := preflightRequest(t, map[string]any{
		"workerId":                "emp_123",
		"currentEmploymentStatus": "active",
		"hireDate":                "2000-03-01",
		"effectiveAt":             "2026-06-30",
		"terminationType":         "retirement",
		"businessReason":          "Employee retirement after 26 years.",
	})

	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if !output.Valid {
		t.Fatalf("expected valid, got errors: %#v", output.Errors)
	}

	if output.RiskLevel == blockshared.RiskHigh {
		t.Fatalf("expected non-high risk for retirement, got %s", output.RiskLevel)
	}
}

func executeInvalidPreflight(t *testing.T, inputFields map[string]any) PreflightOutput {
	t.Helper()

	request := preflightRequest(t, inputFields)
	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected block success (not error), got: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if output.Valid {
		t.Fatalf("expected invalid output, but got valid")
	}

	return output
}

func preflightRequest(t *testing.T, inputFields map[string]any) executor.ExecutionRequest {
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

func assertValidationCode(t *testing.T, issues []ValidationMessage, code string) {
	t.Helper()

	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}

	t.Fatalf("expected validation code %q not found in issues: %#v", code, issues)
}

func assertWarningCode(t *testing.T, warnings []ValidationMessage, code string) {
	t.Helper()

	for _, warning := range warnings {
		if warning.Code == code {
			return
		}
	}

	t.Fatalf("expected warning code %q not found in warnings: %#v", code, warnings)
}
