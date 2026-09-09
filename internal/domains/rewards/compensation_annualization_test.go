package rewards

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func testDecimal(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return d
}

func testMoney(t *testing.T, text, currency string, scale int32) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, currency, scale, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("money %q %s: %v", text, currency, err)
	}
	return m
}

func testAnnualizationRule(t *testing.T) CompensationAnnualizationRule {
	t.Helper()
	return CompensationAnnualizationRule{
		Version:       "rewards.annualization/2.0.0",
		HoursPerWeek:  testDecimal(t, "40.00", 2),
		DaysPerWeek:   testDecimal(t, "5.00", 2),
		WeeksPerYear:  testDecimal(t, "52.00", 2),
		MonthsPerYear: testDecimal(t, "12.00", 2),
		Currency:      "USD",
		MoneyScale:    2,
		MoneyRounding: values.RoundingHalfEven,
	}
}

func testPackage(t *testing.T, amount string, basis PayBasis) CompensationPackage {
	t.Helper()
	return CompensationPackage{Amount: testMoney(t, amount, "USD", 2), Basis: basis}
}

func TestTodo_COMP_002(t *testing.T) {
	rule := testAnnualizationRule(t)
	result, err := AnnualizeCompensation(testPackage(t, "25.00", PayBasisHourly), rule)
	if err != nil {
		t.Fatalf("AnnualizeCompensation: %v", err)
	}
	if got := result.Annualized.String(); got != "52000.00 USD" {
		t.Fatalf("annualized = %s, want 52000.00 USD", got)
	}
	if result.Factor.String() != "2080.0000" {
		t.Fatalf("factor = %s, want 2080.0000", result.Factor)
	}
	if result.RuleVersion != rule.Version || result.MoneyRounding != values.RoundingHalfEven {
		t.Fatalf("result did not retain rule coordinates: %+v", result)
	}
	if result.InputsDigest == "" || result.ResultDigest == "" || result.Canonical() == nil {
		t.Fatal("annualization must retain canonical input/result evidence")
	}
}

func TestTodo_COMP_002_Golden(t *testing.T) {
	rule := testAnnualizationRule(t)
	cases := []struct {
		name   string
		pkg    CompensationPackage
		amount string
	}{
		{"hourly", testPackage(t, "25.00", PayBasisHourly), "52000.00 USD"},
		{"daily", testPackage(t, "400.00", PayBasisDaily), "104000.00 USD"},
		{"monthly", testPackage(t, "10000.00", PayBasisMonthlySalary), "120000.00 USD"},
		{"annual", testPackage(t, "120000.00", PayBasisAnnualSalary), "120000.00 USD"},
		{"piece rate", CompensationPackage{
			Amount: testMoney(t, "12.50", "USD", 2), Basis: PayBasisPieceRate,
			ExpectedUnits: testDecimal(t, "400.00", 2), ExpectedUnit: "EACH",
		}, "260000.00 USD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := AnnualizeCompensation(tc.pkg, rule)
			if err != nil {
				t.Fatalf("AnnualizeCompensation: %v", err)
			}
			if got := result.Annualized.String(); got != tc.amount {
				t.Fatalf("annualized = %s, want %s", got, tc.amount)
			}
		})
	}
}

func TestTodo_COMP_002_Property(t *testing.T) {
	rule := testAnnualizationRule(t)
	result, err := AnnualizeCompensation(CompensationPackage{
		Amount: testMoney(t, "12.50", "USD", 2), Basis: PayBasisPieceRate,
		ExpectedUnits: testDecimal(t, "400.00", 2), ExpectedUnit: "EACH",
	}, rule)
	if err != nil {
		t.Fatalf("piece annualization: %v", err)
	}
	explanation := result.Explain()
	if len(explanation.Factors) != 2 || explanation.Factors[0].Name != "expected_units_per_week:EACH" || explanation.Factors[1].Name != "weeks_per_year" {
		t.Fatalf("Explain factors = %+v", explanation.Factors)
	}
	if explanation.MoneyRounding != values.RoundingHalfEven.String() {
		t.Fatalf("Explain rounding = %q", explanation.MoneyRounding)
	}
	if _, err := AnnualizeCompensation(testPackage(t, "1.00", PayBasisUnspecified), rule); !errors.Is(err, ErrAnnualizationBasisRuleMissing) {
		t.Fatalf("missing basis error = %v, want ErrAnnualizationBasisRuleMissing", err)
	}
	if _, err := AnnualizeCompensation(CompensationPackage{Amount: testMoney(t, "1.00", "USD", 2), Basis: PayBasisPieceRate}, rule); !errors.Is(err, ErrAnnualizationBasisRuleMissing) {
		t.Fatalf("missing piece rule error = %v, want ErrAnnualizationBasisRuleMissing", err)
	}
}

func TestTodo_COMP_002_Race(t *testing.T) {
	rule := testAnnualizationRule(t)
	pkg := testPackage(t, "25.00", PayBasisHourly)
	want, err := AnnualizeCompensation(pkg, rule)
	if err != nil {
		t.Fatalf("seed annualization: %v", err)
	}
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				got, err := AnnualizeCompensation(pkg, rule)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(got.Canonical(), want.Canonical()) {
					errs <- errors.New("concurrent annualization changed canonical bytes")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestTodo_COMP_002_Mutation(t *testing.T) {
	rule := testAnnualizationRule(t)
	pkg := CompensationPackage{Amount: testMoney(t, "10.005", "USD", 3), Basis: PayBasisAnnualSalary}
	result, err := AnnualizeCompensation(pkg, rule)
	if err != nil {
		t.Fatalf("rounding boundary: %v", err)
	}
	if got := result.Annualized.Amount().String(); got != "10.00" {
		t.Fatalf("half-even annualized = %s, want 10.00", got)
	}
	other := rule
	other.Version = "rewards.annualization/2.0.1"
	repeated, err := AnnualizeCompensation(pkg, other)
	if err != nil {
		t.Fatalf("changed rule version: %v", err)
	}
	if repeated.InputsDigest == result.InputsDigest {
		t.Fatal("changing the declared rule version must change the input digest")
	}
}

func testPositionBand(t *testing.T) payband.Band {
	t.Helper()
	return payband.Band{
		ID: "BAND-OPS-P3-USEAST", Version: "2026.1",
		Scope:   payband.Scope{JobCode: "OPS-HRBP3", Grade: "P3", PayZone: "US-EAST"},
		Minimum: testMoney(t, "92000.00", "USD", 2), Midpoint: testMoney(t, "112000.00", "USD", 2), Maximum: testMoney(t, "132000.00", "USD", 2),
	}
}

func testPositionAuth() PayBandPositionAuthorization {
	return PayBandPositionAuthorization{
		PolicyVersion: "rewards.policy/1", Purpose: "compensation-review",
		SubjectDisclosable: true, Scopes: []string{PayBandPositionReadScope},
	}
}

func TestTodo_COMP_003(t *testing.T) {
	result, err := EvaluateCompensationPayBandPosition(PayBandPositionRequest{
		Band: testPositionBand(t), Annualized: testMoney(t, "112000.00", "USD", 2), Authorization: testPositionAuth(),
	})
	if err != nil {
		t.Fatalf("EvaluateCompensationPayBandPosition: %v", err)
	}
	if result.Class != PositionClassWithin || result.Boundary != PositionBoundaryNone {
		t.Fatalf("class/boundary = %s/%s", result.Class, result.Boundary)
	}
	if result.CompaRatio.String() != "1.0000" || result.RangePenetration.String() != "0.5000" {
		t.Fatalf("ratios = %s/%s", result.CompaRatio, result.RangePenetration)
	}
	if result.Disclosure != people.DisclosureFull || result.Explain().Class != PositionClassWithin {
		t.Fatalf("disclosure/explanation = %s/%+v", result.Disclosure, result.Explain())
	}
}

func TestTodo_COMP_003_Property(t *testing.T) {
	band := testPositionBand(t)
	for _, tc := range []struct {
		amount   string
		class    PositionClass
		boundary PositionBoundary
	}{
		{"85000.00", PositionClassBelow, PositionBoundaryMinimum},
		{"140000.00", PositionClassAbove, PositionBoundaryMaximum},
	} {
		result, err := EvaluateCompensationPayBandPosition(PayBandPositionRequest{
			Band: band, Annualized: testMoney(t, tc.amount, "USD", 2), Authorization: testPositionAuth(),
		})
		if err != nil {
			t.Fatalf("amount %s: %v", tc.amount, err)
		}
		if result.Class != tc.class || result.Boundary != tc.boundary {
			t.Errorf("amount %s class/boundary = %s/%s, want %s/%s", tc.amount, result.Class, result.Boundary, tc.class, tc.boundary)
		}
	}
	denied := testPositionAuth()
	denied.Fields = map[PositionField]PositionFieldRuling{
		PositionFieldCompaRatio: {Effect: people.AccessDenied, Reason: "policy:ratio"},
	}
	result, err := EvaluateCompensationPayBandPosition(PayBandPositionRequest{
		Band: band, Annualized: testMoney(t, "112000.00", "USD", 2), Authorization: denied,
	})
	if err != nil {
		t.Fatalf("field-denied position: %v", err)
	}
	if result.Disclosure != people.DisclosurePartial || result.CompaRatioField.Access != people.AccessDenied || result.CompaRatio.Validate() == nil {
		t.Fatalf("field denial was not preserved: %+v", result)
	}
	if got := result.Explain(); got.CompaRatio.Validate() == nil || !strings.Contains(got.Fields[4].DenialReason, "policy:ratio") {
		t.Fatalf("denied Explain leaked or lost reason: %+v", got)
	}
}

func TestTodo_COMP_003_Race(t *testing.T) {
	req := PayBandPositionRequest{Band: testPositionBand(t), Annualized: testMoney(t, "117500.00", "USD", 2), Authorization: testPositionAuth()}
	want, err := EvaluateCompensationPayBandPosition(req)
	if err != nil {
		t.Fatalf("seed position: %v", err)
	}
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				got, err := EvaluateCompensationPayBandPosition(req)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(got.Canonical(), want.Canonical()) {
					errs <- errors.New("concurrent position evaluation changed canonical bytes")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestTodo_COMP_003_Mutation(t *testing.T) {
	band := testPositionBand(t)
	euro := PayBandPositionRequest{Band: band, Annualized: testMoney(t, "100000.00", "EUR", 2), Authorization: testPositionAuth()}
	if _, err := EvaluateCompensationPayBandPosition(euro); !errors.Is(err, ErrPositionCurrencyMismatch) {
		t.Fatalf("currency error = %v, want ErrPositionCurrencyMismatch", err)
	}
	withheld := PayBandPositionRequest{Band: band, Annualized: testMoney(t, "112000.00", "USD", 2), Authorization: PayBandPositionAuthorization{
		PolicyVersion: "rewards.policy/1", Purpose: "compensation-review", SubjectDisclosable: true,
	}}
	result, err := EvaluateCompensationPayBandPosition(withheld)
	if err != nil {
		t.Fatalf("withheld position: %v", err)
	}
	if result.Disclosure != people.DisclosureWithheld || result.BandID != "" || result.CompaRatio.Validate() == nil || result.Explain().BandID != "" {
		t.Fatalf("withheld result disclosed protected data: %+v", result)
	}
}
