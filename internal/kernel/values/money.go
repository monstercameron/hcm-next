package values

import (
	"errors"
	"fmt"
)

// Money, quantity and rate errors. All are matchable with errors.Is.
var (
	ErrCurrencyCode     = errors.New("values: currency is not an uppercase ISO-4217 alphabetic code")
	ErrCurrencyMismatch = errors.New("values: operands are in different currencies")
	ErrUnitCode         = errors.New("values: unit is not an uppercase unit code")
	ErrUnitMismatch     = errors.New("values: operands are in different units")
	ErrRateKind         = errors.New("values: rate numerator is of a different kind")
	ErrMoneyUnset       = errors.New("values: money is unset")
	ErrQuantityUnset    = errors.New("values: quantity is unset")
	ErrRateUnset        = errors.New("values: rate is unset")
	ErrPercentageUnset  = errors.New("values: percentage is unset")
)

// validateCurrency accepts exactly three uppercase ASCII letters. Binding the
// code to the ISO-4217 registry, including minor-unit profiles, is a later
// ticket; the shape check here exists so that a lowercase or padded code can
// never produce a second canonical encoding of the same currency.
func validateCurrency(code string) error {
	if len(code) != 3 {
		return fmt.Errorf("%w: %q", ErrCurrencyCode, code)
	}
	for i := 0; i < 3; i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return fmt.Errorf("%w: %q", ErrCurrencyCode, code)
		}
	}
	return nil
}

// validateUnit accepts an uppercase unit code such as HOUR, DAY or EACH.
func validateUnit(code string) error {
	if code == "" || len(code) > 16 {
		return fmt.Errorf("%w: %q", ErrUnitCode, code)
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_'):
		default:
			return fmt.Errorf("%w: %q", ErrUnitCode, code)
		}
	}
	return nil
}

// Money is an amount in exactly one currency. There is no implicit tenant or
// worker currency, and there is no implicit conversion: adding two amounts in
// different currencies is an error, never a silent FX decision.
//
// The zero Money is unset and fails Validate.
type Money struct {
	amount   Decimal
	currency string
}

// NewMoney parses an amount at a declared scale and rounding mode in the given
// uppercase ISO-4217 currency.
func NewMoney(text, currency string, scale int32, mode RoundingMode) (Money, error) {
	if err := validateCurrency(currency); err != nil {
		return Money{}, err
	}
	amount, err := NewDecimal(text, scale, mode)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: currency}, nil
}

// NewMoneyFromDecimal wraps an existing decimal in a currency.
func NewMoneyFromDecimal(amount Decimal, currency string) (Money, error) {
	if err := amount.Validate(); err != nil {
		return Money{}, err
	}
	if err := validateCurrency(currency); err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: currency}, nil
}

// Validate reports whether the amount is usable.
func (m Money) Validate() error {
	if m.currency == "" {
		return ErrMoneyUnset
	}
	if err := validateCurrency(m.currency); err != nil {
		return err
	}
	return m.amount.Validate()
}

// Amount returns the decimal amount.
func (m Money) Amount() Decimal { return m.amount }

// Currency returns the uppercase ISO-4217 code.
func (m Money) Currency() string { return m.currency }

// String returns "<amount> <CURRENCY>".
func (m Money) String() string {
	if m.Validate() != nil {
		return ""
	}
	return m.amount.String() + " " + m.currency
}

// Canonical returns the canonical byte encoding: the decimal encoding followed
// by the three uppercase currency bytes. It is nil when invalid.
func (m Money) Canonical() []byte {
	if m.Validate() != nil {
		return nil
	}
	return append(m.amount.Canonical(), m.currency...)
}

// MarshalText implements encoding.TextMarshaler.
func (m Money) MarshalText() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	text, err := m.amount.MarshalText()
	if err != nil {
		return nil, err
	}
	return append(append(text, ' '), m.currency...), nil
}

// requireSameCurrency is the single gate that keeps an FX decision explicit.
func (m Money) requireSameCurrency(o Money) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := o.Validate(); err != nil {
		return err
	}
	if m.currency != o.currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, o.currency)
	}
	return nil
}

// Add returns m+o. The currencies and the declared scales must match.
func (m Money) Add(o Money) (Money, error) {
	if err := m.requireSameCurrency(o); err != nil {
		return Money{}, err
	}
	sum, err := m.amount.Add(o.amount)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: sum, currency: m.currency}, nil
}

// Sub returns m-o under the same rules as Add.
func (m Money) Sub(o Money) (Money, error) {
	if err := m.requireSameCurrency(o); err != nil {
		return Money{}, err
	}
	diff, err := m.amount.Sub(o.amount)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: diff, currency: m.currency}, nil
}

// Cmp compares two amounts in the same currency. Comparing across currencies is
// an error, because the answer would depend on an unstated FX rate.
func (m Money) Cmp(o Money) (int, error) {
	if err := m.requireSameCurrency(o); err != nil {
		return 0, err
	}
	return m.amount.Cmp(o.amount), nil
}

// Neg returns -m.
func (m Money) Neg() (Money, error) {
	if err := m.Validate(); err != nil {
		return Money{}, err
	}
	neg, err := m.amount.Neg()
	if err != nil {
		return Money{}, err
	}
	return Money{amount: neg, currency: m.currency}, nil
}

// MulDecimal scales an amount by a dimensionless decimal at an explicitly
// declared result scale and rounding mode. The currency is unchanged.
func (m Money) MulDecimal(d Decimal, scale int32, mode RoundingMode) (Money, error) {
	if err := m.Validate(); err != nil {
		return Money{}, err
	}
	product, err := m.amount.Mul(d, scale, mode)
	if err != nil {
		return Money{}, err
	}
	return Money{amount: product, currency: m.currency}, nil
}

// Percentage is a fraction where 1 means one hundred percent. Storing the
// fraction rather than the percent keeps every downstream multiplication a
// plain decimal multiply with no hidden division by one hundred.
//
// The zero Percentage is unset and fails Validate.
type Percentage struct {
	fraction Decimal
}

// NewPercentage builds a percentage from its fraction, where "0.075" is 7.5%.
func NewPercentage(fractionText string, scale int32, mode RoundingMode) (Percentage, error) {
	fraction, err := NewDecimal(fractionText, scale, mode)
	if err != nil {
		return Percentage{}, err
	}
	return Percentage{fraction: fraction}, nil
}

// NewPercentageFromPercent builds a percentage from percent-denominated text,
// where "7.5" is 7.5%. The declared scale applies to the resulting fraction.
func NewPercentageFromPercent(percentText string, scale int32, mode RoundingMode) (Percentage, error) {
	percent, err := NewDecimal(percentText, scale, mode)
	if err != nil {
		return Percentage{}, err
	}
	hundred, err := NewDecimal("100", 0, mode)
	if err != nil {
		return Percentage{}, err
	}
	fraction, err := percent.Div(hundred, scale, mode)
	if err != nil {
		return Percentage{}, err
	}
	return Percentage{fraction: fraction}, nil
}

// Validate reports whether the percentage is usable.
func (p Percentage) Validate() error {
	if err := p.fraction.Validate(); err != nil {
		if errors.Is(err, ErrDecimalUnset) {
			return ErrPercentageUnset
		}
		return err
	}
	return nil
}

// Fraction returns the underlying fraction, where 1 is one hundred percent.
func (p Percentage) Fraction() Decimal { return p.fraction }

// String returns the fraction as plain text.
func (p Percentage) String() string { return p.fraction.String() }

// Canonical returns the canonical byte encoding of the fraction.
func (p Percentage) Canonical() []byte { return p.fraction.Canonical() }

// ApplyTo multiplies an amount by the percentage at an explicitly declared
// result scale and rounding mode.
func (p Percentage) ApplyTo(m Money, scale int32, mode RoundingMode) (Money, error) {
	if err := p.Validate(); err != nil {
		return Money{}, err
	}
	return m.MulDecimal(p.fraction, scale, mode)
}

// Quantity is a decimal with a unit code. Adding two quantities in different
// units is an error; unit conversion is an explicit, versioned decision.
//
// The zero Quantity is unset and fails Validate.
type Quantity struct {
	value Decimal
	unit  string
}

// NewQuantity parses a value at a declared scale and rounding mode in the given
// uppercase unit code.
func NewQuantity(text, unit string, scale int32, mode RoundingMode) (Quantity, error) {
	if err := validateUnit(unit); err != nil {
		return Quantity{}, err
	}
	value, err := NewDecimal(text, scale, mode)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{value: value, unit: unit}, nil
}

// Validate reports whether the quantity is usable.
func (q Quantity) Validate() error {
	if q.unit == "" {
		return ErrQuantityUnset
	}
	if err := validateUnit(q.unit); err != nil {
		return err
	}
	return q.value.Validate()
}

// Value returns the decimal value.
func (q Quantity) Value() Decimal { return q.value }

// Unit returns the unit code.
func (q Quantity) Unit() string { return q.unit }

// String returns "<value> <UNIT>".
func (q Quantity) String() string {
	if q.Validate() != nil {
		return ""
	}
	return q.value.String() + " " + q.unit
}

// Canonical returns the decimal encoding followed by the unit code bytes.
func (q Quantity) Canonical() []byte {
	if q.Validate() != nil {
		return nil
	}
	return append(q.value.Canonical(), q.unit...)
}

func (q Quantity) requireSameUnit(o Quantity) error {
	if err := q.Validate(); err != nil {
		return err
	}
	if err := o.Validate(); err != nil {
		return err
	}
	if q.unit != o.unit {
		return fmt.Errorf("%w: %s vs %s", ErrUnitMismatch, q.unit, o.unit)
	}
	return nil
}

// Add returns q+o. The units and the declared scales must match.
func (q Quantity) Add(o Quantity) (Quantity, error) {
	if err := q.requireSameUnit(o); err != nil {
		return Quantity{}, err
	}
	sum, err := q.value.Add(o.value)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{value: sum, unit: q.unit}, nil
}

// Sub returns q-o under the same rules as Add.
func (q Quantity) Sub(o Quantity) (Quantity, error) {
	if err := q.requireSameUnit(o); err != nil {
		return Quantity{}, err
	}
	diff, err := q.value.Sub(o.value)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{value: diff, unit: q.unit}, nil
}

// rateKind names which numerator a rate carries.
type rateKind uint8

const (
	rateKindUnset rateKind = iota
	rateKindMoney
	rateKindQuantity
)

// Rate is an amount per unit: money per unit for pay rates, or quantity per
// unit for accrual rates. The denominator unit is always explicit, so applying
// a rate to a quantity in another unit is an error rather than a guess.
//
// The zero Rate is unset and fails Validate.
type Rate struct {
	kind     rateKind
	money    Money
	quantity Quantity
	perUnit  string
}

// NewMoneyRate builds a money-per-unit rate, such as 24.00 USD per HOUR.
func NewMoneyRate(amount Money, perUnit string) (Rate, error) {
	if err := amount.Validate(); err != nil {
		return Rate{}, err
	}
	if err := validateUnit(perUnit); err != nil {
		return Rate{}, err
	}
	return Rate{kind: rateKindMoney, money: amount, perUnit: perUnit}, nil
}

// NewQuantityRate builds a quantity-per-unit rate, such as 0.0385 HOUR of leave
// accrual per HOUR worked.
func NewQuantityRate(amount Quantity, perUnit string) (Rate, error) {
	if err := amount.Validate(); err != nil {
		return Rate{}, err
	}
	if err := validateUnit(perUnit); err != nil {
		return Rate{}, err
	}
	return Rate{kind: rateKindQuantity, quantity: amount, perUnit: perUnit}, nil
}

// Validate reports whether the rate is usable.
func (r Rate) Validate() error {
	switch r.kind {
	case rateKindMoney:
		if err := r.money.Validate(); err != nil {
			return err
		}
	case rateKindQuantity:
		if err := r.quantity.Validate(); err != nil {
			return err
		}
	default:
		return ErrRateUnset
	}
	return validateUnit(r.perUnit)
}

// PerUnit returns the denominator unit code.
func (r Rate) PerUnit() string { return r.perUnit }

// MoneyNumerator returns the money numerator and whether the rate carries one.
func (r Rate) MoneyNumerator() (Money, bool) { return r.money, r.kind == rateKindMoney }

// QuantityNumerator returns the quantity numerator and whether the rate carries
// one.
func (r Rate) QuantityNumerator() (Quantity, bool) {
	return r.quantity, r.kind == rateKindQuantity
}

// String returns "<numerator>/<UNIT>".
func (r Rate) String() string {
	if r.Validate() != nil {
		return ""
	}
	if r.kind == rateKindMoney {
		return r.money.String() + "/" + r.perUnit
	}
	return r.quantity.String() + "/" + r.perUnit
}

// Canonical returns the numerator encoding followed by the denominator unit.
func (r Rate) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	if r.kind == rateKindMoney {
		return append(r.money.Canonical(), r.perUnit...)
	}
	return append(r.quantity.Canonical(), r.perUnit...)
}

// Apply multiplies a money rate by a quantity in the rate's denominator unit,
// at an explicitly declared result scale and rounding mode.
func (r Rate) Apply(q Quantity, scale int32, mode RoundingMode) (Money, error) {
	if err := r.Validate(); err != nil {
		return Money{}, err
	}
	if r.kind != rateKindMoney {
		return Money{}, fmt.Errorf("%w: rate numerator is a quantity", ErrRateKind)
	}
	if err := q.Validate(); err != nil {
		return Money{}, err
	}
	if q.unit != r.perUnit {
		return Money{}, fmt.Errorf("%w: rate is per %s, quantity is in %s", ErrUnitMismatch, r.perUnit, q.unit)
	}
	return r.money.MulDecimal(q.value, scale, mode)
}

// ApplyQuantity multiplies a quantity rate by a quantity in the rate's
// denominator unit, at an explicitly declared result scale and rounding mode.
func (r Rate) ApplyQuantity(q Quantity, scale int32, mode RoundingMode) (Quantity, error) {
	if err := r.Validate(); err != nil {
		return Quantity{}, err
	}
	if r.kind != rateKindQuantity {
		return Quantity{}, fmt.Errorf("%w: rate numerator is money", ErrRateKind)
	}
	if err := q.Validate(); err != nil {
		return Quantity{}, err
	}
	if q.unit != r.perUnit {
		return Quantity{}, fmt.Errorf("%w: rate is per %s, quantity is in %s", ErrUnitMismatch, r.perUnit, q.unit)
	}
	product, err := r.quantity.value.Mul(q.value, scale, mode)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{value: product, unit: r.quantity.unit}, nil
}
