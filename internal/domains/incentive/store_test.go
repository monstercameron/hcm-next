package incentive

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMemoryStorePreservesTenantScopedRevisionRules(t *testing.T) {
	plan := testMemoryPlan(t)
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.SavePlan(ctx, "tenant-a", plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(ctx, "tenant-a", plan); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate plan error = %v, want ErrStoreDuplicate", err)
	}
	if _, err := store.LoadPlan(ctx, "tenant-b", plan.PlanID, plan.Revision); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant plan load = %v, want ErrStoreNotFound", err)
	}
}

func testMemoryPlan(t *testing.T) IncentivePlanRevision {
	t.Helper()
	plan, err := NewIncentivePlanRevision(IncentivePlanRevision{
		PlanID: "plan", Revision: 1, Name: "Plan", Currency: "USD", PeriodRef: "FY26",
		EligibilityRef: "eligible", FormulaRef: "formula", Measures: []PlanMeasure{
			{ID: "measure", Name: "Measure", Kind: MeasureRevenue, Target: testMemoryDecimal(t, "100.00"), Weight: testMemoryDecimal(t, "100.00")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testMemoryDecimal(t *testing.T, text string) (d values.Decimal) {
	t.Helper()
	var err error
	d, err = values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
