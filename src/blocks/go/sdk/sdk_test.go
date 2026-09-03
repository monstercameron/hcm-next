package sdk

import (
	"testing"
)

// TestRouteKeyConstants pins the public route-key values the SDK exposes:
// customer blocks build integrations against these exact strings, so a silent
// rename here is a breaking change the test must catch.
func TestRouteKeyConstants(t *testing.T) {
	values := map[string]string{
		RouteKeyValidationFailed:     "validation_failed",
		RouteKeyApprovalRequired:     "approval_required",
		RouteKeyApprovalWithWarnings: "approval_with_warnings",
		RouteKeyNoApprovalRequired:   "no_approval_required",
		RouteKeyTransactionPlanReady: "transaction_plan_ready",
	}
	for got, want := range values {
		if got != want {
			t.Errorf("route key constant = %q, want %q", got, want)
		}
	}
}

type nameInput struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

// TestDecodeInput proves the deterministic decode contract: known fields
// decode, missing fields zero-fill, unknown fields and malformed bytes are
// rejected as typed invalid-input errors naming the contract.
func TestDecodeInput(t *testing.T) {
	if input, err := DecodeInput[nameInput]([]byte(`{"first":"Ada","last":"Lovelace"}`), "Legal name"); err != nil {
		t.Fatalf("DecodeInput on valid input: %v", err)
	} else if input.First != "Ada" || input.Last != "Lovelace" {
		t.Errorf("DecodeInput = %+v, want Ada/Lovelace", input)
	}

	if _, err := DecodeInput[nameInput]([]byte(`{}`), "Legal name"); err != nil {
		t.Fatalf("DecodeInput on empty object: %v, want zero-filled success", err)
	}

	if _, err := DecodeInput[nameInput]([]byte(`{"first":"Ada","unexpected":1}`), "Legal name"); err == nil {
		t.Error("DecodeInput accepted an unknown field, want rejection")
	} else if err.Code != "invalid_input" {
		t.Errorf("unknown field rejection code = %q, want invalid_input (the public wire value)", err.Code)
	} else if err.Message != "Legal name input does not match the expected contract." {
		t.Errorf("rejection message = %q, want the contract-named message", err.Message)
	}

	if _, err := DecodeInput[nameInput]([]byte(`{"first":5}`), "Legal name"); err == nil {
		t.Error("DecodeInput accepted a wrong-typed field, want rejection")
	}

	if _, err := DecodeInput[nameInput]([]byte(`not json`), "Legal name"); err == nil {
		t.Error("DecodeInput accepted malformed JSON, want rejection")
	}

	if _, err := DecodeInput[nameInput](nil, "Legal name"); err == nil {
		t.Error("DecodeInput accepted empty input, want rejection")
	}
}

// TestRouteKeyForValidation proves the SDK route-key decision order: invalid
// wins over everything, warnings win over the approval requirement, then the
// approval requirement, then the straight-through path.
func TestRouteKeyForValidation(t *testing.T) {
	cases := []struct {
		name             string
		isValid          bool
		warningCount     int
		requiresApproval bool
		want             string
	}{
		{"invalid wins over approval", false, 0, true, RouteKeyValidationFailed},
		{"invalid wins over warnings", false, 3, false, RouteKeyValidationFailed},
		{"warnings win over approval required", true, 2, true, RouteKeyApprovalWithWarnings},
		{"warnings win over approval not required", true, 1, false, RouteKeyApprovalWithWarnings},
		{"approval required", true, 0, true, RouteKeyApprovalRequired},
		{"straight through", true, 0, false, RouteKeyNoApprovalRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RouteKeyForValidation(tc.isValid, tc.warningCount, tc.requiresApproval); got != tc.want {
				t.Errorf("RouteKeyForValidation(%v, %d, %v) = %q, want %q", tc.isValid, tc.warningCount, tc.requiresApproval, got, tc.want)
			}
		})
	}
}

// TestNewValidationOutputContract proves the validation builder carries the
// given fields through untouched and normalizes nil lists to empty (JSON
// non-null) lists, while leaving the transaction plan absent.
func TestNewValidationOutputContract(t *testing.T) {
	facts := []Fact{{Key: "employeeId", Value: "emp-1", Source: "request"}}
	errors := []ValidationIssue{{Code: "required", Field: "legalName", Message: "required"}}
	warnings := []ValidationIssue{{Code: "review", Field: "effectiveAt", Message: "far future"}}

	contract := NewValidationOutputContract(RouteKeyApprovalRequired, facts, errors, warnings)
	if contract.RouteKey != RouteKeyApprovalRequired {
		t.Errorf("RouteKey = %q", contract.RouteKey)
	}
	if len(contract.Facts) != 1 || contract.Facts[0].Key != "employeeId" || contract.Facts[0].Value != "emp-1" {
		t.Errorf("Facts = %+v, want the given fact", contract.Facts)
	}
	if len(contract.ValidationErrors) != 1 || contract.ValidationErrors[0].Code != "required" {
		t.Errorf("ValidationErrors = %+v, want the given issue", contract.ValidationErrors)
	}
	if len(contract.Warnings) != 1 || contract.Warnings[0].Field != "effectiveAt" {
		t.Errorf("Warnings = %+v, want the given warning", contract.Warnings)
	}
	if contract.TransactionPlan != nil {
		t.Errorf("validation contract must not carry a transaction plan, got %+v", contract.TransactionPlan)
	}
	if len(contract.ExternalCalls) != 0 || len(contract.LedgerFacts) != 0 || len(contract.ProjectionPatches) != 0 {
		t.Errorf("validation contract must carry empty side-effect lists, got calls=%d ledgerFacts=%d patches=%d",
			len(contract.ExternalCalls), len(contract.LedgerFacts), len(contract.ProjectionPatches))
	}

	empty := NewValidationOutputContract(RouteKeyNoApprovalRequired, nil, nil, nil)
	if len(empty.Facts) != 0 || len(empty.ValidationErrors) != 0 || len(empty.Warnings) != 0 {
		t.Errorf("nil inputs must normalize to empty, non-nil slices: facts=%v errors=%v warnings=%v",
			empty.Facts, empty.ValidationErrors, empty.Warnings)
	}
	if empty.Facts == nil || empty.ValidationErrors == nil || empty.Warnings == nil {
		t.Error("nil inputs must normalize to empty non-nil slices")
	}
}

// TestNewTransactionOutputContract proves the transaction builder carries all
// eight given arguments through to both the plan and the top-level contract
// fields, and normalizes nil lists to empty (JSON non-null) lists.
func TestNewTransactionOutputContract(t *testing.T) {
	facts := []Fact{{Key: "changeRequestId", Value: "cr-1"}}
	ledgerFacts := []LedgerFact{{EventType: "legal_name.changed", SubjectType: "employee", SubjectID: "emp-1", EffectiveAt: "2026-09-03T00:00:00Z"}}
	externalCalls := []ExternalCallRequest{{ConnectionID: "conn-1", Operation: "notify", IdempotencyKey: "idem-1"}}
	projectionPatches := []ProjectionPatch{{Projection: "employee_record", Operation: "set", Path: "/legalName", Value: "Ada Lovelace"}}
	operations := []TransactionOperation{{Operation: "write", Target: "employee.legal_name", IdempotencyKey: "idem-1"}}

	contract := NewTransactionOutputContract(
		RouteKeyTransactionPlanReady,
		facts,
		ledgerFacts,
		externalCalls,
		projectionPatches,
		"employee_legal_name_change",
		"cr-1:emp-1:2026-09-03",
		operations,
	)

	if contract.RouteKey != RouteKeyTransactionPlanReady {
		t.Errorf("RouteKey = %q", contract.RouteKey)
	}
	plan := contract.TransactionPlan
	if plan == nil {
		t.Fatal("transaction plan is nil")
	}
	if plan.PlanType != "employee_legal_name_change" || plan.IdempotencyKey != "cr-1:emp-1:2026-09-03" {
		t.Errorf("plan identity = %q/%q", plan.PlanType, plan.IdempotencyKey)
	}
	if len(plan.LedgerFacts) != 1 || len(plan.ExternalCalls) != 1 || len(plan.ProjectionPatches) != 1 || len(plan.Operations) != 1 {
		t.Errorf("plan lists = ledgerFacts %d calls %d patches %d operations %d, want 1 each",
			len(plan.LedgerFacts), len(plan.ExternalCalls), len(plan.ProjectionPatches), len(plan.Operations))
	}
	if len(contract.Facts) != 1 || len(contract.LedgerFacts) != 1 || len(contract.ExternalCalls) != 1 || len(contract.ProjectionPatches) != 1 {
		t.Error("top-level contract must mirror facts and the plan's side-effect lists")
	}
	if len(contract.ValidationErrors) != 0 || len(contract.Warnings) != 0 {
		t.Error("transaction contract must carry empty validation lists")
	}

	empty := NewTransactionOutputContract(RouteKeyApprovalRequired, nil, nil, nil, nil, "plan", "key", nil)
	if empty.TransactionPlan == nil {
		t.Fatal("empty transaction contract must still carry a plan")
	}
	if empty.TransactionPlan.LedgerFacts == nil || empty.TransactionPlan.ExternalCalls == nil ||
		empty.TransactionPlan.ProjectionPatches == nil || empty.TransactionPlan.Operations == nil {
		t.Error("nil inputs must normalize to empty non-nil plan lists")
	}
	if empty.Facts == nil {
		t.Error("nil facts must normalize to an empty non-nil slice")
	}
	if empty.LedgerFacts == nil || empty.ExternalCalls == nil || empty.ProjectionPatches == nil {
		t.Error("nil inputs must normalize to empty non-nil top-level lists")
	}
}
