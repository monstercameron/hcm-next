package auditpack

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Reconcile checks the two exact relationships a sound payroll run's four
// totals must satisfy, at whatever decimal scale each was declared under,
// with no tolerance:
//
//   - REGISTER_VS_BANK_FILE_PLUS_TAX_LIABILITY: the register total (gross pay)
//     equals the bank file total (net pay disbursed) plus the tax liability
//     total (amount withheld). Gross pay has exactly two destinations; a
//     third would be an unexplained variance by definition.
//   - TAX_LIABILITY_VS_FILING_ACKNOWLEDGMENT: the tax liability total equals
//     the filing acknowledgment total. What was withheld is what a filing
//     authority confirmed receiving.
//
// Every pair that fails to agree exactly is named in the returned
// [Decision]; [Decision.Err] turns that into one or more
// [ErrVarianceUnexplained]. There is no tolerance parameter: a payroll amount
// that is off by any nonzero difference is exactly the failure this todo
// exists to catch, not a rounding artifact to be waved through.
func Reconcile(totals RunTotals) (Decision, error) {
	register, ok := totals.Total(KindRegister)
	if !ok {
		return Decision{}, ErrMissingTotal{RunID: totals.RunID, Kind: KindRegister}
	}
	bankFile, ok := totals.Total(KindBankFile)
	if !ok {
		return Decision{}, ErrMissingTotal{RunID: totals.RunID, Kind: KindBankFile}
	}
	taxLiability, ok := totals.Total(KindTaxLiability)
	if !ok {
		return Decision{}, ErrMissingTotal{RunID: totals.RunID, Kind: KindTaxLiability}
	}
	filingAck, ok := totals.Total(KindFilingAcknowledgment)
	if !ok {
		return Decision{}, ErrMissingTotal{RunID: totals.RunID, Kind: KindFilingAcknowledgment}
	}

	disbursedPlusWithheld, err := addWidened(bankFile, taxLiability)
	if err != nil {
		return Decision{}, fmt.Errorf("auditpack: sum bank file and tax liability: %w", err)
	}
	pairOne, err := buildPair("REGISTER_VS_BANK_FILE_PLUS_TAX_LIABILITY", KindRegister,
		[]TotalKind{KindBankFile, KindTaxLiability}, register, disbursedPlusWithheld)
	if err != nil {
		return Decision{}, err
	}

	pairTwo, err := buildPair("TAX_LIABILITY_VS_FILING_ACKNOWLEDGMENT", KindTaxLiability,
		[]TotalKind{KindFilingAcknowledgment}, taxLiability, filingAck)
	if err != nil {
		return Decision{}, err
	}

	return Decision{Pairs: []PairResult{pairOne, pairTwo}}, nil
}

func buildPair(name string, left TotalKind, right []TotalKind, leftAmount, rightAmount values.Decimal) (PairResult, error) {
	diff, err := subWidened(leftAmount, rightAmount)
	if err != nil {
		return PairResult{}, fmt.Errorf("auditpack: compare %s: %w", name, err)
	}
	diff, err = diff.Abs()
	if err != nil {
		return PairResult{}, fmt.Errorf("auditpack: compare %s: %w", name, err)
	}
	return PairResult{
		Name: name, Left: left, Right: right,
		LeftAmount: leftAmount, RightAmount: rightAmount,
		Difference: diff, OK: leftAmount.Cmp(rightAmount) == 0,
	}, nil
}

// widen returns a and b re-declared at the wider of their two scales. Scale
// is never narrowed here, so this can never discard a digit: widening a
// decimal's declared scale is exact by construction
// (values.Decimal.Quantize), it only changes how many trailing zeros are
// written.
func widen(a, b values.Decimal) (values.Decimal, values.Decimal, error) {
	if a.Scale() == b.Scale() {
		return a, b, nil
	}
	scale := a.Scale()
	if b.Scale() > scale {
		scale = b.Scale()
	}
	aw, err := a.Quantize(scale, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	bw, err := b.Quantize(scale, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, values.Decimal{}, err
	}
	return aw, bw, nil
}

func addWidened(a, b values.Decimal) (values.Decimal, error) {
	aw, bw, err := widen(a, b)
	if err != nil {
		return values.Decimal{}, err
	}
	return aw.Add(bw)
}

func subWidened(a, b values.Decimal) (values.Decimal, error) {
	aw, bw, err := widen(a, b)
	if err != nil {
		return values.Decimal{}, err
	}
	return aw.Sub(bw)
}
