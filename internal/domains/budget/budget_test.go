package budget

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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

// BenchmarkTodo_BUDGET_001 is the registry's required budget observation
// benchmark. Keep the operation representative of the read path: validation
// plus canonical evidence encoding.
func BenchmarkTodo_BUDGET_001(b *testing.B) {
	ref := validRefForBenchmark(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := ref.Validate(); err != nil {
			b.Fatal(err)
		}
		if len(ref.Canonical()) == 0 {
			b.Fatal("valid reference has no canonical encoding")
		}
	}
}

func validRefForBenchmark(b *testing.B) BudgetAuthorityRef {
	b.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	amount, err := values.NewDecimal("100.00", 2, values.RoundingExactRequired)
	if err != nil {
		b.Fatal(err)
	}
	return BudgetAuthorityRef{
		BudgetType: CompensationPool, OwnerSystem: "finance.erp", Scope: "cost-center:eng",
		Period: "FY2026", Currency: "USD", Unit: UnitMoney, BaselineVersion: "baseline/7",
		AvailableQuantity: amount,
		Evidence:          ObservationEvidence{ObservationID: "obs-1", SourceWatermark: values.NewInstant(now.Add(-time.Hour)), RetrievedAt: values.NewInstant(now), Digest: "sha256:abc"},
	}
}

func TestTodoBUDGET001Mutation(t *testing.T) {
	base := validRef(t)
	baseCanonical := base.Canonical()
	if len(baseCanonical) == 0 {
		t.Fatal("valid reference has no canonical encoding")
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
			if got := ref.Canonical(); string(got) == string(baseCanonical) {
				t.Fatal("material observation mutation did not change canonical encoding")
			}
		})
	}
}

func TestTodoBUDGET001RejectsAsOfBeforeSourceWatermark(t *testing.T) {
	ref := validRef(t)
	err := ref.Fresh(ref.Evidence.SourceWatermark.Time().Add(-time.Nanosecond), time.Hour)
	if !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("error = %v, want ErrInvalidObservation", err)
	}
}
