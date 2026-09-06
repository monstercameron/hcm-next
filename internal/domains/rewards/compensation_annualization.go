package rewards

import (
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// PayBasisDaily and PayBasisPieceRate extend the compensation basis vocabulary
// for the standalone annualization contract. The original simulation contract
// predates these bases, so this file deliberately owns their handling rather
// than changing the existing simulation wire vocabulary.
const (
	PayBasisDaily     PayBasis = 4
	PayBasisPieceRate PayBasis = 5
	PayBasisPiece     PayBasis = PayBasisPieceRate
	PayBasisAnnual    PayBasis = PayBasisAnnualSalary
	PayBasisMonthly   PayBasis = PayBasisMonthlySalary
)

var (
	// ErrCompensationAnnualizationInvalid is returned for an unusable package
	// or rule set.
	ErrCompensationAnnualizationInvalid = errors.New("rewards: compensation annualization input is invalid")
	// ErrAnnualizationBasisRuleMissing is returned when a basis has no
	// declared factor, such as a piece rate without expected units.
	ErrAnnualizationBasisRuleMissing = errors.New("rewards: annualization basis has no declared rule")
	// ErrAnnualizationCurrencyMismatch is returned instead of making an FX
	// decision from an ambient worker or band currency.
	ErrAnnualizationCurrencyMismatch = errors.New("rewards: annualization currency mismatch")
)

// CompensationPackage is the closed amount-plus-basis input annualized by
// COMP-002. ExpectedUnits is the declared number of pieces per week for a
// piece-rate package; it is otherwise absent and never inferred.
type CompensationPackage struct {
	Amount        values.Money
	Basis         PayBasis
	ExpectedUnits values.Decimal
	ExpectedUnit  string
}

func compensationBasisString(basis PayBasis) string {
	switch basis {
	case PayBasisHourly:
		return "HOURLY"
	case PayBasisDaily:
		return "DAILY"
	case PayBasisMonthlySalary:
		return "MONTHLY"
	case PayBasisAnnualSalary:
		return "ANNUAL"
	case PayBasisPieceRate:
		return "PIECE_RATE"
	default:
		return "PAY_BASIS_UNSPECIFIED"
	}
}

func (p CompensationPackage) Validate() error {
	if err := p.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %w", ErrCompensationAnnualizationInvalid, err)
	}
	switch p.Basis {
	case PayBasisHourly, PayBasisDaily, PayBasisMonthlySalary, PayBasisAnnualSalary:
	case PayBasisPieceRate:
		if err := p.ExpectedUnits.Validate(); err != nil {
			return fmt.Errorf("%w: piece expected units: %w", ErrAnnualizationBasisRuleMissing, err)
		}
		if p.ExpectedUnits.Sign() <= 0 || p.ExpectedUnit == "" {
			return fmt.Errorf("%w: piece rate requires positive expected units and a unit", ErrAnnualizationBasisRuleMissing)
		}
		if _, err := values.NewQuantity("1", p.ExpectedUnit, 0, values.RoundingHalfEven); err != nil {
			return fmt.Errorf("%w: piece unit: %w", ErrAnnualizationBasisRuleMissing, err)
		}
	default:
		return fmt.Errorf("%w: basis %s", ErrAnnualizationBasisRuleMissing, compensationBasisString(p.Basis))
	}
	return nil
}

// Canonical returns the canonical package encoding, or nil when invalid.
func (p CompensationPackage) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.rewards.CompensationPackageAnnualization", rewardsSchemaVer).
		Value("amount", p.Amount).
		String("basis", compensationBasisString(p.Basis)).
		Bool("expected_units?", p.Basis == PayBasisPieceRate)
	if p.Basis == PayBasisPieceRate {
		w.Value("expected_units", p.ExpectedUnits).String("expected_unit", p.ExpectedUnit)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CompensationAnnualizationRule is the declared rule set for COMP-002.
// DaysPerWeek is required for daily pay and MonthsPerYear for monthly pay;
// keeping both explicit prevents a calendar or a conventional constant from
// silently changing a result.
type CompensationAnnualizationRule struct {
	Version       string
	HoursPerWeek  values.Decimal
	DaysPerWeek   values.Decimal
	WeeksPerYear  values.Decimal
	MonthsPerYear values.Decimal
	Currency      string
	MoneyScale    int32
	MoneyRounding values.RoundingMode
}

// AnnualizationRuleSet is the descriptive alias used by callers composing a
// rule set independently from a compensation package.
type AnnualizationRuleSet = CompensationAnnualizationRule

// CompensationAnnualizationRules is a plural descriptive alias retained for
// callers that model a declared rule set as a collection of factors.
type CompensationAnnualizationRules = CompensationAnnualizationRule

func (r CompensationAnnualizationRule) Validate() error {
	if r.Version == "" || r.Currency == "" {
		return fmt.Errorf("%w: version and currency are required", ErrCompensationAnnualizationInvalid)
	}
	// NewMoney is used only as the kernel's public currency-shape validator.
	if _, err := values.NewMoney("0", r.Currency, 0, values.RoundingHalfEven); err != nil {
		return fmt.Errorf("%w: currency: %w", ErrCompensationAnnualizationInvalid, err)
	}
	factors := []struct {
		name  string
		value values.Decimal
	}{
		{name: "hours_per_week", value: r.HoursPerWeek},
		{name: "days_per_week", value: r.DaysPerWeek},
		{name: "weeks_per_year", value: r.WeeksPerYear},
		{name: "months_per_year", value: r.MonthsPerYear},
	}
	for _, factor := range factors {
		if err := factor.value.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrCompensationAnnualizationInvalid, factor.name, err)
		}
		if factor.value.Sign() <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrCompensationAnnualizationInvalid, factor.name)
		}
	}
	if r.MoneyScale < 0 || r.MoneyScale > values.MaxScale {
		return fmt.Errorf("%w: money scale %d", ErrCompensationAnnualizationInvalid, r.MoneyScale)
	}
	if !r.MoneyRounding.Valid() || r.MoneyRounding == values.RoundingUnspecified {
		return fmt.Errorf("%w: money rounding is unspecified", ErrCompensationAnnualizationInvalid)
	}
	return nil
}

// Canonical returns the canonical rule encoding, or nil when invalid.
func (r CompensationAnnualizationRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.CompensationAnnualizationRule", rewardsSchemaVer).
		String("version", r.Version).
		Value("hours_per_week", r.HoursPerWeek).
		Value("days_per_week", r.DaysPerWeek).
		Value("weeks_per_year", r.WeeksPerYear).
		Value("months_per_year", r.MonthsPerYear).
		String("currency", r.Currency).
		Int("money_scale", int64(r.MoneyScale)).
		String("money_rounding", r.MoneyRounding.String()).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// AnnualizationFactor is one named multiplicative input shown by Explain.
type AnnualizationFactor struct {
	Name  string
	Value values.Decimal
}

// AnnualizationResult is the exact annualized amount and its replay trace.
type AnnualizationResult struct {
	Package       CompensationPackage
	Annualized    values.Money
	Factor        values.Decimal
	Factors       []AnnualizationFactor
	RuleVersion   string
	Currency      string
	MoneyScale    int32
	MoneyRounding values.RoundingMode
	InputsDigest  string
	ResultDigest  string
}

func exactProduct(a, b values.Decimal) (values.Decimal, error) {
	scale := a.Scale() + b.Scale()
	if scale > values.MaxScale {
		return values.Decimal{}, fmt.Errorf("%w: factor scale %d exceeds %d", ErrCompensationAnnualizationInvalid, scale, values.MaxScale)
	}
	return a.Mul(b, scale, values.RoundingExactRequired)
}

func oneDecimal() values.Decimal {
	return values.MustDecimal("1", 0, values.RoundingExactRequired)
}

func (r CompensationAnnualizationRule) factorFor(p CompensationPackage) (values.Decimal, []AnnualizationFactor, error) {
	one := oneDecimal()
	factors := []AnnualizationFactor{}
	switch p.Basis {
	case PayBasisAnnualSalary:
		factors = append(factors, AnnualizationFactor{Name: "annual_amount", Value: one})
	case PayBasisMonthlySalary:
		factors = append(factors, AnnualizationFactor{Name: "months_per_year", Value: r.MonthsPerYear})
		return r.MonthsPerYear, factors, nil
	case PayBasisHourly:
		factors = append(factors,
			AnnualizationFactor{Name: "hours_per_week", Value: r.HoursPerWeek},
			AnnualizationFactor{Name: "weeks_per_year", Value: r.WeeksPerYear})
		factor, err := exactProduct(r.HoursPerWeek, r.WeeksPerYear)
		return factor, factors, err
	case PayBasisDaily:
		factors = append(factors,
			AnnualizationFactor{Name: "days_per_week", Value: r.DaysPerWeek},
			AnnualizationFactor{Name: "weeks_per_year", Value: r.WeeksPerYear})
		factor, err := exactProduct(r.DaysPerWeek, r.WeeksPerYear)
		return factor, factors, err
	case PayBasisPieceRate:
		factors = append(factors,
			AnnualizationFactor{Name: "expected_units_per_week:" + p.ExpectedUnit, Value: p.ExpectedUnits},
			AnnualizationFactor{Name: "weeks_per_year", Value: r.WeeksPerYear})
		factor, err := exactProduct(p.ExpectedUnits, r.WeeksPerYear)
		return factor, factors, err
	default:
		return values.Decimal{}, nil, fmt.Errorf("%w: basis %s", ErrAnnualizationBasisRuleMissing, compensationBasisString(p.Basis))
	}
	return one, factors, nil
}

func (r CompensationAnnualizationRule) canonicalInput(p CompensationPackage) (string, error) {
	return canonicalbytes.New("hcmnext.domains.rewards.AnnualizeCompensationInput", rewardsSchemaVer).
		Value("package", p).Value("rule", r).Digest()
}

func (a AnnualizationResult) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.rewards.AnnualizationResult", rewardsSchemaVer).
		Value("package", a.Package).
		Value("annualized", a.Annualized).
		Value("factor", a.Factor).
		String("rule_version", a.RuleVersion).
		String("currency", a.Currency).
		Int("money_scale", int64(a.MoneyScale)).
		String("money_rounding", a.MoneyRounding.String()).
		Count("factor_count", len(a.Factors))
	for _, factor := range a.Factors {
		w.String("factor.name", factor.Name).Value("factor.value", factor.Value)
	}
	return w.Bytes()
}

// Canonical returns the result envelope, or nil when its result body cannot
// be encoded.
func (a AnnualizationResult) Canonical() []byte {
	body, err := a.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.AnnualizationResultEnvelope", rewardsSchemaVer).
		Field("body", body).String("inputs_digest", a.InputsDigest).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// AnnualizeCompensation computes a package's annual amount with no clock,
// calendar, database or external side effect.
func AnnualizeCompensation(p CompensationPackage, r CompensationAnnualizationRule) (AnnualizationResult, error) {
	if err := p.Validate(); err != nil {
		return AnnualizationResult{}, err
	}
	if err := r.Validate(); err != nil {
		return AnnualizationResult{}, err
	}
	if p.Amount.Currency() != r.Currency {
		return AnnualizationResult{}, fmt.Errorf("%w: package %s, rule %s", ErrAnnualizationCurrencyMismatch, p.Amount.Currency(), r.Currency)
	}
	factor, factors, err := r.factorFor(p)
	if err != nil {
		return AnnualizationResult{}, err
	}
	annualized, err := p.Amount.MulDecimal(factor, r.MoneyScale, r.MoneyRounding)
	if err != nil {
		return AnnualizationResult{}, fmt.Errorf("%w: annualized amount: %w", ErrCompensationAnnualizationInvalid, err)
	}
	inputs, err := r.canonicalInput(p)
	if err != nil {
		return AnnualizationResult{}, err
	}
	result := AnnualizationResult{
		Package: p, Annualized: annualized, Factor: factor, Factors: factors,
		RuleVersion: r.Version, Currency: r.Currency, MoneyScale: r.MoneyScale,
		MoneyRounding: r.MoneyRounding, InputsDigest: inputs,
	}
	body, err := result.canonicalBody()
	if err != nil {
		return AnnualizationResult{}, err
	}
	result.ResultDigest = canonicalbytes.Digest(body)
	return result, nil
}

// Annualize is a concise alias for AnnualizeCompensation.
func Annualize(p CompensationPackage, r CompensationAnnualizationRule) (AnnualizationResult, error) {
	return AnnualizeCompensation(p, r)
}

// AnnualizePackage is a descriptive alias for AnnualizeCompensation.
func AnnualizePackage(p CompensationPackage, r CompensationAnnualizationRule) (AnnualizationResult, error) {
	return AnnualizeCompensation(p, r)
}

// Annualize computes the annualized amount using this package as input.
func (p CompensationPackage) Annualize(r CompensationAnnualizationRule) (AnnualizationResult, error) {
	return AnnualizeCompensation(p, r)
}

// AnnualizationExplanation is the disclosure-safe trace for an annualization.
// It lists factors and rule coordinates but does not invent calendar facts.
type AnnualizationExplanation struct {
	Basis         string
	Currency      string
	RuleVersion   string
	MoneyScale    int32
	MoneyRounding string
	Factors       []AnnualizationFactor
	InputsDigest  string
	ResultDigest  string
}

func (a AnnualizationResult) Explain() AnnualizationExplanation {
	return AnnualizationExplanation{
		Basis: compensationBasisString(a.Package.Basis), Currency: a.Currency,
		RuleVersion: a.RuleVersion, MoneyScale: a.MoneyScale,
		MoneyRounding: a.MoneyRounding.String(), Factors: append([]AnnualizationFactor(nil), a.Factors...),
		InputsDigest: a.InputsDigest, ResultDigest: a.ResultDigest,
	}
}

// ExplainAnnualization is the package-level Explain-shaped entry point.
func ExplainAnnualization(a AnnualizationResult) AnnualizationExplanation { return a.Explain() }
