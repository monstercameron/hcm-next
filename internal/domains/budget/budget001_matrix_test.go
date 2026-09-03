package budget

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_BUDGET_001 is the registry's exact primary matrix symbol.
func TestTodo_BUDGET_001(t *testing.T) {
	ref := validRef(t)
	if err := ref.Validate(); err != nil {
		t.Fatalf("valid typed observation rejected: %v", err)
	}
	if got := ref.Canonical(); len(got) == 0 {
		t.Fatal("valid typed observation has no canonical encoding")
	}
	if err := ref.Fresh(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), time.Hour); err != nil {
		t.Fatalf("fresh typed observation rejected: %v", err)
	}

	stale := ref
	stale.Evidence.SourceWatermark = values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := stale.Fresh(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), time.Hour); !errors.Is(err, ErrStale) {
		t.Fatalf("stale observation error = %v, want ErrStale", err)
	}
}

// TestTodo_BUDGET_001_Mutation is the registry's exact mutation matrix symbol.
func TestTodo_BUDGET_001_Mutation(t *testing.T) {
	base := validRef(t)
	baseCanonical := string(base.Canonical())
	if baseCanonical == "" {
		t.Fatal("valid typed observation has no canonical encoding")
	}

	cases := []struct {
		name   string
		mutate func(*BudgetAuthorityRef)
	}{
		{"budget type", func(r *BudgetAuthorityRef) { r.BudgetType = HeadcountCapacity; r.Unit = UnitFTE; r.Currency = "" }},
		{"owner", func(r *BudgetAuthorityRef) { r.OwnerSystem = "finance.other" }},
		{"scope", func(r *BudgetAuthorityRef) { r.Scope = "cost-center:ops" }},
		{"period", func(r *BudgetAuthorityRef) { r.Period = "FY2027" }},
		{"quantity", func(r *BudgetAuthorityRef) {
			r.AvailableQuantity, _ = values.NewDecimal("101.00", 2, values.RoundingExactRequired)
		}},
		{"source watermark", func(r *BudgetAuthorityRef) {
			r.Evidence.SourceWatermark = values.NewInstant(time.Date(2026, 9, 3, 11, 30, 0, 0, time.UTC))
		}},
		{"retrieved at", func(r *BudgetAuthorityRef) {
			r.Evidence.RetrievedAt = values.NewInstant(time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC))
		}},
		{"observation id", func(r *BudgetAuthorityRef) { r.Evidence.ObservationID = "obs-2" }},
		{"digest", func(r *BudgetAuthorityRef) { r.Evidence.Digest = "sha256:def" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := base
			tc.mutate(&ref)
			if got := string(ref.Canonical()); got == baseCanonical {
				t.Fatal("material observation mutation did not change canonical encoding")
			}
		})
	}
}
