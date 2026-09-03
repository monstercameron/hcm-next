package budget

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func validRef(t *testing.T) BudgetAuthorityRef {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	amount, err := values.NewDecimal("100.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return BudgetAuthorityRef{
		BudgetType: CompensationPool, OwnerSystem: "finance.erp", Scope: "cost-center:eng",
		Period: "FY2026", Currency: "USD", Unit: UnitMoney, BaselineVersion: "baseline/7",
		AvailableQuantity: amount,
		Evidence:          ObservationEvidence{ObservationID: "obs-1", SourceWatermark: values.NewInstant(now.Add(-time.Hour)), RetrievedAt: values.NewInstant(now), Digest: "sha256:abc"},
	}
}

func TestTodoBUDGET001TypedObservation(t *testing.T) {
	ref := validRef(t)
	if err := ref.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(ref.Canonical()) == 0 {
		t.Fatal("valid reference has no canonical encoding")
	}
	if err := ref.Fresh(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestTodoBUDGET001RejectsWrongTypeUnitCurrencyAndStale(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*BudgetAuthorityRef)
		want   error
	}{
		{"wrong unit", func(r *BudgetAuthorityRef) { r.Unit = UnitFTE; r.Currency = "" }, ErrWrongUnit},
		{"currency on headcount", func(r *BudgetAuthorityRef) { r.BudgetType = HeadcountCapacity; r.Unit = UnitFTE }, ErrWrongCurrency},
		{"missing money currency", func(r *BudgetAuthorityRef) { r.Currency = "" }, ErrWrongCurrency},
		{"stale source", func(r *BudgetAuthorityRef) {
			r.Evidence.SourceWatermark = values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		}, ErrStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := validRef(t)
			tc.mutate(&ref)
			if tc.want == ErrStale {
				if err := ref.Fresh(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), time.Hour); !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
				return
			}
			if err := ref.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodoBUDGET001RejectsNegativeAndExhausted(t *testing.T) {
	for _, text := range []string{"-1.00", "0.00"} {
		t.Run(text, func(t *testing.T) {
			ref := validRef(t)
			amount, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
			if err != nil {
				t.Fatal(err)
			}
			ref.AvailableQuantity = amount
			if err := ref.Validate(); err == nil {
				t.Fatal("invalid quantity accepted")
			}
		})
	}
}
