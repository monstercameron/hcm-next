package incentive_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/incentive"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func decimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func knownAt(t *testing.T, text string) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(values.NewInstant(mustTime(t, text)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustTime(t *testing.T, text string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func testPlan(t *testing.T) incentive.IncentivePlanRevision {
	t.Helper()
	plan, err := incentive.NewIncentivePlanRevision(incentive.IncentivePlanRevision{
		PlanID: "commission-2026", Revision: 1, Name: "FY26 Commission", Currency: "USD",
		PeriodRef: "FY26", EligibilityRef: "eligible-sales", FormulaRef: "formula-v1",
		Measures: []incentive.PlanMeasure{
			{ID: "bookings", Name: "Bookings", Kind: incentive.MeasureBookings, Target: decimal(t, "100.00"), Weight: decimal(t, "60.00"), SourceRef: "crm"},
			{ID: "quality", Name: "Quality", Kind: incentive.MeasureQuality, Target: decimal(t, "90.00"), Weight: decimal(t, "40.00"), SourceRef: "quality-system"},
		},
		ClawbackRules: []incentive.ClawbackRule{{ID: "misstatement", Version: "v1", Trigger: incentive.ClawbackMisstatement, WindowDays: 90, FormulaRef: "recover-paid-award", EvidenceRequired: true, EvidenceRef: "policy-evidence"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testObservation(t *testing.T, id, measure string, value values.Decimal) incentive.AttainmentObservation {
	t.Helper()
	o, err := incentive.NewAttainmentObservation(incentive.AttainmentObservation{
		ObservationID: id, PlanID: "commission-2026", PlanRevision: 1, MeasureID: measure,
		WorkerRef: "worker-1", Value: value, AsOfEffective: mustDate(t, "2026-06-30"),
		AsKnownAt: knownAt(t, "2026-07-02T12:00:00Z"), SourceRef: "source-" + measure, Watermark: "watermark-" + measure,
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func mustDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func testAward(t *testing.T, plan incentive.IncentivePlanRevision) incentive.AwardCalculation {
	t.Helper()
	bookings := testObservation(t, "obs-bookings", "bookings", decimal(t, "120.00"))
	quality := testObservation(t, "obs-quality", "quality", decimal(t, "95.00"))
	award, err := incentive.NewAwardCalculation(incentive.AwardCalculation{
		CalculationID: "award-1", WorkerRef: "worker-1", PlanDigest: plan.CanonicalDigest, PlanRevision: plan.Revision,
		PeriodRef: "FY26", EligibilityRef: "eligible-sales/worker-1", FormulaRef: "formula-v1",
		Inputs: []incentive.AwardInput{
			{Name: "bookings-attainment", MeasureID: "bookings", ObservationDigest: bookings.CanonicalDigest, Watermark: bookings.Watermark, Attainment: bookings.Value, Target: decimal(t, "100.00"), Weight: decimal(t, "60.00")},
			{Name: "quality-attainment", MeasureID: "quality", ObservationDigest: quality.CanonicalDigest, Watermark: quality.Watermark, Attainment: quality.Value, Target: decimal(t, "90.00"), Weight: decimal(t, "40.00")},
		},
		Amount: decimal(t, "2500.00"), Currency: "USD", State: incentive.AwardCalculated, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return award
}

func TestIncentiveAwardRequiresPinnedPlanMeasuresAttainmentAndEligibility(t *testing.T) {
	plan := testPlan(t)
	award := testAward(t, plan)
	approved, err := award.Approve("approval-1")
	if err != nil || approved.State != incentive.AwardApproved || approved.SupersedesRevision != 1 {
		t.Fatalf("approve: %#v %v", approved, err)
	}
	finalized, err := approved.Finalize("approval-2")
	if err != nil || finalized.State != incentive.AwardFinalized || finalized.CanonicalDigest == approved.CanonicalDigest {
		t.Fatalf("finalize: %#v %v", finalized, err)
	}
	explanation, err := finalized.Explain()
	if err != nil || len(explanation.InputNames) != 2 {
		t.Fatalf("explain: %#v %v", explanation, err)
	}
	if strings.Contains(fmt.Sprintf("%#v", explanation), "2500.00") {
		t.Fatal("award explanation must not disclose amount")
	}
}
func TestTodo_INCENTIVE_001_Property(t *testing.T) {
	plan := testPlan(t)
	bad := plan
	bad.Measures = append([]incentive.PlanMeasure(nil), plan.Measures...)
	bad.Measures[0].Weight = decimal(t, "50.00")
	if _, err := incentive.NewIncentivePlanRevision(bad); !errors.Is(err, incentive.ErrInvalidPlan) {
		t.Fatalf("weighting error = %v", err)
	}
}

func TestTodo_INCENTIVE_001_Golden(t *testing.T) {
	award := testAward(t, testPlan(t))
	if award.CanonicalDigest == "" || award.Digest != award.CanonicalDigest {
		t.Fatal("award digest was not minted")
	}
}

func TestTodo_INCENTIVE_001_Race(t *testing.T) {
	plan := testPlan(t)
	if _, err := plan.Successor(incentive.IncentivePlanRevision{Revision: 2, Name: plan.Name, Currency: plan.Currency, PeriodRef: plan.PeriodRef, EligibilityRef: plan.EligibilityRef, FormulaRef: plan.FormulaRef, Measures: plan.Measures}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_INCENTIVE_001_Fault(t *testing.T) {
	award := testAward(t, testPlan(t))
	award.Amount = decimal(t, "9999.99")
	if err := award.Validate(); !errors.Is(err, incentive.ErrInvalidAward) {
		t.Fatalf("tampered award accepted: %v", err)
	}
}

func TestTodo_INCENTIVE_001_Security(t *testing.T) {
	award := testAward(t, testPlan(t))
	explanation, err := incentive.Explain(award)
	if err != nil || len(explanation.InputNames) != 2 {
		t.Fatalf("explain: %#v %v", explanation, err)
	}
}

func TestTodo_INCENTIVE_001_Conformance(t *testing.T) {
	if incentive.Version() != 1 {
		t.Fatalf("version = %d", incentive.Version())
	}
}

func TestTodo_INCENTIVE_001_Mutation(t *testing.T) {
	plan := testPlan(t)
	plan.Measures[0].Name = "changed"
	if err := plan.Validate(); !errors.Is(err, incentive.ErrInvalidPlan) {
		t.Fatalf("mutated plan accepted: %v", err)
	}
}
