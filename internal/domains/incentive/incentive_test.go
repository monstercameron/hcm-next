package incentive_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/incentive"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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

type approvalVerifier struct {
	claim incentive.ApprovalClaim
	ref   string
}

func (v approvalVerifier) VerifyApproval(got incentive.ApprovalClaim, ref string) error {
	if ref != v.ref || got.CalculationID != v.claim.CalculationID || got.WorkerRef != v.claim.WorkerRef || got.PeriodRef != v.claim.PeriodRef || got.Currency != v.claim.Currency || got.AwardDigest != v.claim.AwardDigest || got.AwardRevision != v.claim.AwardRevision || !got.Amount.Equal(v.claim.Amount) {
		return errors.New("receipt mismatch")
	}
	return nil
}
func verifierForCalculated(a incentive.AwardCalculation, ref string) approvalVerifier {
	return approvalVerifier{ref: ref, claim: incentive.ApprovalClaim{CalculationID: a.CalculationID, WorkerRef: a.WorkerRef, PeriodRef: a.PeriodRef, Currency: a.Currency, Amount: a.Amount, AwardDigest: a.Digest, AwardRevision: a.Revision}}
}

func TestIncentiveAwardRequiresPinnedPlanMeasuresAttainmentAndEligibility(t *testing.T) {
	plan := testPlan(t)
	award := testAward(t, plan)
	approved, err := award.Approve("approval-1", verifierForCalculated(award, "approval-1"))
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
	if got, want := award.CanonicalDigest, "sha256:b06644a86d9160a818eb8f51ca971073445eda1b922204971b37e8146d190989"; got != want {
		t.Fatalf("award canonical digest = %q, want %q", got, want)
	}
}

func TestTodo_INCENTIVE_001_Race(t *testing.T) {
	plan := testPlan(t)
	const callers = 32
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := plan.Successor(incentive.IncentivePlanRevision{Revision: 2, Name: plan.Name, Currency: plan.Currency, PeriodRef: plan.PeriodRef, EligibilityRef: plan.EligibilityRef, FormulaRef: plan.FormulaRef, Measures: plan.Measures})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
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

func TestVocabularyCanonicalAndExplanationSurface(t *testing.T) {
	plan := testPlan(t)
	if len(plan.Canonical()) == 0 {
		t.Fatal("plan canonical is empty")
	}
	digest, err := plan.DigestValue()
	if err != nil || digest != plan.Digest {
		t.Fatalf("plan digest = %q, err=%v", digest, err)
	}
	explanation, err := incentive.ExplainPlan(plan)
	if err != nil || explanation.Digest != plan.Digest || len(explanation.MeasureIDs) != 2 {
		t.Fatalf("plan explanation = %#v, err=%v", explanation, err)
	}
	copyPlan, err := incentive.NewPlanRevision(incentive.IncentivePlanRevision{
		PlanID: plan.PlanID, Revision: 1, Name: plan.Name, Currency: plan.Currency,
		PeriodRef: plan.PeriodRef, EligibilityRef: plan.EligibilityRef, FormulaRef: plan.FormulaRef,
		Measures: plan.Measures, ClawbackRules: plan.ClawbackRules,
	})
	if err != nil || copyPlan.Digest != plan.Digest {
		t.Fatalf("plan alias constructor = %#v, err=%v", copyPlan, err)
	}
	successor, err := plan.Successor(incentive.IncentivePlanRevision{Revision: 2, Name: plan.Name, Currency: plan.Currency, PeriodRef: plan.PeriodRef, EligibilityRef: plan.EligibilityRef, FormulaRef: plan.FormulaRef, Measures: plan.Measures, ClawbackRules: plan.ClawbackRules})
	if err != nil || successor.ParentDigest != plan.Digest || successor.SupersedesRevision != 1 {
		t.Fatalf("successor = %#v, err=%v", successor, err)
	}

	observation := testObservation(t, "canonical-observation", "bookings", decimal(t, "101.00"))
	if len(observation.Canonical()) == 0 {
		t.Fatal("observation canonical is empty")
	}
	threshold := incentive.AwardThreshold{Name: "floor", Minimum: decimal(t, "80.00"), Multiplier: decimal(t, "1.00")}
	if err := threshold.Validate(); err != nil || len(threshold.Canonical()) == 0 {
		t.Fatalf("threshold = %#v, err=%v", threshold, err)
	}
	award := testAward(t, plan)
	award.Thresholds = []incentive.AwardThreshold{threshold}
	award, err = incentive.NewAward(award)
	if err != nil || len(award.Canonical()) == 0 {
		t.Fatalf("award alias constructor = %#v, err=%v", award, err)
	}
	if _, err := award.Finalize("too-soon"); !errors.Is(err, incentive.ErrAwardTransition) {
		t.Fatalf("calculated award finalized directly: %v", err)
	}
}
