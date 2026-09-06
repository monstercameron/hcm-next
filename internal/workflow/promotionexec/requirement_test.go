package promotionexec

import (
	"testing"
	"time"
)

// Both compiled approval requirements refuse the two inputs a caller could
// leave out: the approver principal and the decision deadline. The positive
// shape of each requirement is pinned by TestTodo_PROMO_EXEC_DEF_ApprovalRequirements
// in definition_test.go; this file owns the refusals.
func TestCompileApprovalRequirementsRefuseAMissingApproverOrDeadline(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for name, compile := range map[string]func(string, time.Time) (interface{}, error){
		"finance": func(a string, d time.Time) (interface{}, error) { return CompileFinanceApprovalRequirement(a, d) },
		"manager": func(a string, d time.Time) (interface{}, error) { return CompileManagerApprovalRequirement(a, d) },
	} {
		if _, err := compile("", when); err == nil {
			t.Fatalf("%s: an empty approver compiled", name)
		}
		if _, err := compile("principal:someone", time.Time{}); err == nil {
			t.Fatalf("%s: a zero deadline compiled", name)
		}
		if _, err := compile("principal:someone", when); err != nil {
			t.Fatalf("%s: a complete requirement was refused: %v", name, err)
		}
	}
}

// The two requirements are distinct decisions: different ids, policies,
// governance refs and authority floors, never one requirement under two names.
func TestFinanceAndManagerRequirementsAreDistinct(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	finance, err := CompileFinanceApprovalRequirement("principal:finance", when)
	if err != nil {
		t.Fatalf("finance: %v", err)
	}
	manager, err := CompileManagerApprovalRequirement("principal:manager", when)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	if finance.RequirementID == manager.RequirementID {
		t.Fatalf("both requirements compiled to id %q", finance.RequirementID)
	}
	if finance.RequirementID != ApprovalFinance || manager.RequirementID != ApprovalManager {
		t.Fatalf("ids = %q, %q; want %q, %q", finance.RequirementID, manager.RequirementID, ApprovalFinance, ApprovalManager)
	}
}
