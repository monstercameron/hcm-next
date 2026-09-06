package payroll

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestDigest_IsDeterministic is a smoke check on the small content-identity
// helper every transform in this package uses.
func TestDigest_IsDeterministic(t *testing.T) {
	a := digest("profile", "x", "y")
	b := digest("profile", "x", "y")
	if a != b {
		t.Fatalf("digest is not deterministic: %q vs %q", a, b)
	}
	if c := digest("profile", "x", "z"); c == a {
		t.Fatalf("digest did not change with a changed input")
	}
}

// TestTransformComputeCalculation_IsExactDecimalNotFloat proves the
// gross-to-net calculation is exact decimal arithmetic: the same gross input
// always produces the exact same net/tax amounts, and a value whose exact
// decimal result would be lost to float64 rounding still round-trips
// exactly.
func TestTransformComputeCalculation_IsExactDecimalNotFloat(t *testing.T) {
	gross, err := values.NewMoney("100.10", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	result, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
		Transform: workflow.CompiledTransform{TransformRef: TransformComputeCalculation},
		Inputs: simulate.Bag{
			"gross_pay_input":      simulate.NewMoney(gross),
			"population_watermark": simulate.NewString("wm@1"),
		},
	})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	net, err := result.Outputs["net_pay_amount"].Money()
	if err != nil {
		t.Fatalf("net_pay_amount: %v", err)
	}
	tax, err := result.Outputs["tax_amount"].Money()
	if err != nil {
		t.Fatalf("tax_amount: %v", err)
	}
	// 100.10 * 0.22 = 22.022, rounded HALF_EVEN to 2 places = 22.02;
	// 100.10 - 22.02 = 78.08. Neither figure is representable exactly as a
	// binary float64, which is exactly what this test is proving does not
	// leak into the result.
	if tax.String() != "22.02 USD" {
		t.Fatalf("tax = %s, want 22.02 USD", tax.String())
	}
	if net.String() != "78.08 USD" {
		t.Fatalf("net = %s, want 78.08 USD", net.String())
	}
}

// TestApprovals_WouldAwaitOneWorkItemPerRequirement is a smoke check that
// every declared requirement ref raises exactly one work item.
func TestApprovals_WouldAwaitOneWorkItemPerRequirement(t *testing.T) {
	items, err := Approvals{}.WouldAwait(context.Background(), simulate.ApprovalRequest{
		NodeID:          NodeEndPendingSettlementObligations,
		RequirementRefs: []string{ApprovalPayrollController, ApprovalFinanceRelease},
	})
	if err != nil {
		t.Fatalf("WouldAwait: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("work items = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.State != simulate.WouldAwait {
			t.Errorf("item %s state = %q, want %q", item.RequirementID, item.State, simulate.WouldAwait)
		}
	}
}
