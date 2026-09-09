package orgtransfer

import (
	"encoding/json"
	"testing"

	"human-capital-management-suite-executor/internal/executor"
)

func TestExecutePreflightProducesGenericRouteKey(t *testing.T) {
	request := orgTransferPreflightRequest(t, orgTransferPreflightFixture())

	blockResult, executionError := ExecutePreflight(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PreflightOutput)
	if !output.Valid {
		t.Fatalf("expected valid output, got errors: %#v", output.Errors)
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

func TestExecutePlanTransactionProducesGenericTransactionOutput(t *testing.T) {
	request := orgTransferPlanRequest(t, orgTransferPlanFixture())

	blockResult, executionError := ExecutePlanTransaction(request)
	if executionError != nil {
		t.Fatalf("expected success, got error: %#v", executionError)
	}

	output := blockResult.Output.(PlanTransactionOutput)
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

	if len(output.TransactionPlan.Operations) != len(output.AssignmentOperations)+len(output.RoleBindingOperations) {
		t.Fatalf("expected assignment and role operations in generic transaction plan")
	}

	if len(output.TransactionPlan.ProjectionPatches) != len(output.ProjectionPatches) {
		t.Fatalf("expected transaction plan projection patches to mirror legacy projection patches")
	}
}

func orgTransferPreflightRequest(t *testing.T, input map[string]any) executor.ExecutionRequest {
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

func orgTransferPlanRequest(t *testing.T, input map[string]any) executor.ExecutionRequest {
	t.Helper()

	encodedInput, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to encode test input: %v", err)
	}

	return executor.ExecutionRequest{
		TenantID:           "tenant_demo",
		EnvironmentID:      "env_demo",
		ChangeRequestID:    "chg_123",
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

func orgTransferPreflightFixture() map[string]any {
	return map[string]any{
		"employee": map[string]any{
			"employeeId":       "emp_123",
			"employmentStatus": "active",
		},
		"currentOrganization": map[string]any{
			"legalEntity":  "US",
			"businessUnit": "Operations",
			"department":   "People",
			"team":         "People Ops",
			"location":     "San Francisco",
			"payZone":      "US-WEST",
			"costCenter":   "CC-100",
		},
		"targetLocationOrgUnit": orgUnitFixture("loc_nyc", "location", "New York", map[string]any{}),
		"targetTeamOrgUnit": orgUnitFixture("team_sales_ops", "team", "Sales Ops", map[string]any{
			"businessUnit": "Sales",
		}),
		"targetCostCenterOrgUnit": orgUnitFixture("cc_200", "cost_center", "CC-200", map[string]any{
			"businessUnit": "Sales",
		}),
		"targetManager": map[string]any{
			"employeeId":       "mgr_456",
			"employmentStatus": "active",
		},
		"activeAssignments": []map[string]any{
			assignmentFixture("wa_team_current", "emp_123", "team_people_ops", "primary_team"),
		},
		"effectiveAt":              "2026-06-01",
		"businessReason":           "org_restructure",
		"transferReason":           "team_reassignment",
		"accessImpactAcknowledged": true,
	}
}

func orgTransferPlanFixture() map[string]any {
	return map[string]any{
		"changeRequestId": "chg_123",
		"workerId":        "emp_123",
		"currentOrganization": map[string]any{
			"legalEntity":  "US",
			"businessUnit": "Operations",
			"department":   "People",
			"team":         "People Ops",
			"location":     "San Francisco",
			"payZone":      "US-WEST",
			"costCenter":   "CC-100",
		},
		"proposedOrganization": map[string]any{
			"legalEntity":  "US",
			"businessUnit": "Sales",
			"department":   "Revenue Operations",
			"team":         "Sales Ops",
			"location":     "New York",
			"payZone":      "US-EAST",
			"costCenter":   "CC-200",
		},
		"currentJob": map[string]any{
			"jobCode": "OPS-HRBP2",
			"title":   "HR Business Partner",
			"family":  "People",
			"level":   "P2",
		},
		"proposedJob": map[string]any{
			"jobCode": "REV-OPS2",
			"title":   "Revenue Operations Partner",
			"family":  "Revenue",
			"level":   "P2",
		},
		"currentCompensation": map[string]any{
			"amount":             93000,
			"currency":           "USD",
			"payFrequency":       "annual",
			"bonusTargetPercent": 5,
			"effectiveDate":      "2026-01-01",
		},
		"proposedCompensation": map[string]any{
			"amount":             98000,
			"currency":           "USD",
			"payFrequency":       "annual",
			"bonusTargetPercent": 7.5,
			"effectiveDate":      "2026-06-01",
		},
		"currentAssignments": []map[string]any{
			assignmentFixture("wa_team_current", "emp_123", "team_people_ops", "primary_team"),
			assignmentFixture("wa_location_current", "emp_123", "loc_sfo", "work_location"),
			assignmentFixture("wa_cost_center_current", "emp_123", "cc_100", "cost_center"),
		},
		"targetTeamOrgUnit":       orgUnitFixture("team_sales_ops", "team", "Sales Ops", map[string]any{"businessUnit": "Sales"}),
		"targetLocationOrgUnit":   orgUnitFixture("loc_nyc", "location", "New York", map[string]any{}),
		"targetCostCenterOrgUnit": orgUnitFixture("cc_200", "cost_center", "CC-200", map[string]any{"businessUnit": "Sales"}),
		"sourceManagerEmployeeId": "mgr_111",
		"targetManagerEmployeeId": "mgr_456",
		"effectiveAt":             "2026-06-01",
		"businessReason":          "org_restructure",
	}
}

func orgUnitFixture(orgUnitID string, orgUnitType string, name string, metadata map[string]any) map[string]any {
	return map[string]any{
		"orgUnitId": orgUnitID,
		"type":      orgUnitType,
		"name":      name,
		"status":    "active",
		"metadata":  metadata,
	}
}

func assignmentFixture(workerAssignmentID string, employeeID string, orgUnitID string, assignmentType string) map[string]any {
	return map[string]any{
		"workerAssignmentId": workerAssignmentID,
		"employeeId":         employeeID,
		"orgUnitId":          orgUnitID,
		"assignmentType":     assignmentType,
		"allocationPercent":  100,
		"status":             "active",
		"effectiveStart":     "2025-01-01",
		"metadata":           map[string]any{},
	}
}
