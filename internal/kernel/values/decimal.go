package values

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

// Fixed-decimal limits. They bound the canonical encoding so that every
// supported binary agrees on what is representable.
const (
	// MaxScale is the largest declared number of fractional digits.
	MaxScale = 18
	// MaxPrecision is the largest number of significant digits in the unscaled
	// integer. Exceeding it is an error, never a wrapped or truncated result.
	MaxPrecision = 38
)

// Decimal errors. All are matchable with errors.Is.
var (
	ErrDecimalUnset        = errors.New("values: decimal is unset")
	ErrDecimalSyntax       = errors.New("values: decimal text is not a plain fixed-point number")
	ErrNotFinite           = errors.New("values: NaN and infinity are not representable")
	ErrNegativeZero        = errors.New("values: negative zero is not representable")
	ErrExcessScale         = errors.New("values: value has more fractional digits than the declared scale")
	ErrScaleRange          = errors.New("values: declared scale is out of range")
	ErrPrecisionOverflow   = errors.New("values: value exceeds the declared maximum precision")
	ErrRoundingUnspecified = errors.New("values: rounding mode is unspecified")
	ErrRoundingMode        = errors.New("values: unknown rounding mode")
	ErrInexact             = errors.New("values: operation would round but EXACT_REQUIRED was declared")
	ErrScaleMismatch       = errors.New("values: operands have different declared scales")
	ErrDivideByZero        = errors.New("values: division by zero")
)

// RoundingMode is the explicitly declared rounding behavior of a value. There
// is no default: an operation that could round without a declared mode is an
// error, because a silently chosen mode is a silently wrong payroll amount.
type RoundingMode uint8

// Rounding modes.
const (
	// RoundingUnspecified is the zero value and is never legal.
	RoundingUnspecified RoundingMode = iota
	// RoundingHalfEven rounds ties to the nearest even digit.
	RoundingHalfEven
	// RoundingHalfUp rounds ties away from zero. "Half up" is the
	// decimal-arithmetic name for that behavior, so -2.5 rounds to -3.
	RoundingHalfUp
	// RoundingHalfAwayFromZero is the explicit spelling of RoundingHalfUp; the
	// wire vocabulary carries both names and they are the same behavior.
	RoundingHalfAwayFromZero
	// RoundingFloor rounds toward negative infinity.
	RoundingFloor
	// RoundingCeiling rounds toward positive infinity.
	RoundingCeiling
	// RoundingTowardZero truncates.
	RoundingTowardZero
	// RoundingAwayFromZero rounds any nonzero discarded digits away from zero.
	RoundingAwayFromZero
	// RoundingExactRequired forbids rounding: an inexact result is an error.
	RoundingExactRequired
)

var roundingWire = map[RoundingMode]string{
	RoundingHalfEven:         "HALF_EVEN",
	RoundingHalfUp:           "HALF_UP",
	RoundingHalfAwayFromZero: "HALF_AWAY_FROM_ZERO",
	RoundingFloor:            "FLOOR",
	RoundingCeiling:          "CEILING",
	RoundingTowardZero:       "TOWARD_ZERO",
	RoundingAwayFromZero:     "AWAY_FROM_ZERO",
	RoundingExactRequired:    "EXACT_REQUIRED",
}

// String returns the stable wire token.
func (m RoundingMode) String() string {
	if w, ok := roundingWire[m]; ok {
		return w
	}
	return "ROUNDING_UNSPECIFIED"
}

// Valid reports whether m is a declared mode.
func (m RoundingMode) Valid() bool {
	_, ok := roundingWire[m]
	return ok
}

// ParseRoundingMode decodes a wire token. The unspecified token is rejected.
func ParseRoundingMode(token string) (RoundingMode, error) {
	for mode, wire := range roundingWire {
		if wire == token {
			return mode, nil
		}
	}
	return RoundingUnspecified, fmt.Errorf("%w: %q", ErrRoundingMode, token)
}

// apdRounder maps a declared mode onto the qualified decimal backend. It is the
// only place an apd rounder is named.
func apdRounder(m RoundingMode) apd.Rounder {
	switch m {
	case RoundingHalfEven:
		return apd.RoundHalfEven
	case RoundingHalfUp, RoundingHalfAwayFromZero:
		return apd.RoundHalfUp
	case RoundingFloor:
		return apd.RoundFloor
	case RoundingCeiling:
		return apd.RoundCeiling
	case RoundingTowardZero:
		return apd.RoundDown
	case RoundingAwayFromZero:
		return apd.RoundUp
	default:
		// EXACT_REQUIRED never reaches the backend: exactness is checked first.
		return apd.RoundHalfEven
	}
}

// newDecimalContext returns a fresh backend context. A context is never shared
// and never stored, so no mutable apd state escapes this package.
func newDecimalContext(m RoundingMode) *apd.Context {
	c := apd.BaseContext.WithPrecision(uint32(MaxPrecision + 10))
	c.Rounding = apdRounder(m)
	return c
}

// Decimal is a fixed-precision decimal with a declared scale and a declared
// rounding mode. It never uses binary floating point, it has no NaN, infinity
// or negative zero, and it refuses to round unless a mode says how.
//
// The zero Decimal is unset and fails Validate.
type Decimal struct {
	set      bool
	neg      bool
	unscaled *big.Int // absolute value; treated as immutable after construction
	scale    int32
	round    RoundingMode
}

// NewDecimal parses plain fixed-point text at a declared scale and rounding
// mode. Text carrying more fractional digits than the declared scale is
// rejected rather than rounded; use Quantize to round explicitly.
func NewDecimal(text string, scale int32, mode RoundingMode) (Decimal, error) {
	if err := checkScale(scale); err != nil {
		return Decimal{}, err
	}
	if err := checkMode(mode); err != nil {
		return Decimal{}, err
	}
	neg, intPart, fracPart, err := parseFixedText(text)
	if err != nil {
		return Decimal{}, err
	}
	if int32(len(fracPart)) > scale {
		return Decimal{}, fmt.Errorf("%w: %q has %d fractional digits, declared scale is %d",
			ErrExcessScale, text, len(fracPart), scale)
	}
	digits := intPart + fracPart + strings.Repeat("0", int(scale)-len(fracPart))
	unscaled, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return Decimal{}, fmt.Errorf("%w: %q", ErrDecimalSyntax, text)
	}
	if neg && unscaled.Sign() == 0 {
		return Decimal{}, fmt.Errorf("%w: %q", ErrNegativeZero, text)
	}
	return newDecimal(neg && unscaled.Sign() != 0, unscaled, scale, mode)
}

// MustDecimal is NewDecimal for compile-time-known constants; it panics on a
// bad literal. Use it only for package-level constants, never on input.
func MustDecimal(text string, scale int32, mode RoundingMode) Decimal {
	d, err := NewDecimal(text, scale, mode)
	if err != nil {
		panic("values: MustDecimal: " + err.Error())
	}
	return d
}

// newDecimal builds a decimal from an already-validated magnitude.
func newDecimal(neg bool, unscaled *big.Int, scale int32, mode RoundingMode) (Decimal, error) {
	if unscaled.Sign() < 0 {
		return Decimal{}, fmt.Errorf("values: internal: magnitude must be non-negative")
	}
	if digits := len(unscaled.String()); digits > MaxPrecision {
		return Decimal{}, fmt.Errorf("%w: %d significant digits exceeds %d",
			ErrPrecisionOverflow, digits, MaxPrecision)
	}
	if unscaled.Sign() == 0 {
		neg = false
	}
	return Decimal{set: true, neg: neg, unscaled: new(big.Int).Set(unscaled), scale: scale, round: mode}, nil
}

// fromSigned builds a decimal from a signed unscaled integer, normalizing away
// negative zero.
func fromSigned(signed *big.Int, scale int32, mode RoundingMode) (Decimal, error) {
	mag := new(big.Int).Abs(signed)
	return newDecimal(signed.Sign() < 0, mag, scale, mode)
}

func checkScale(scale int32) error {
	if scale < 0 || scale > MaxScale {
		return fmt.Errorf("%w: %d is outside [0,%d]", ErrScaleRange, scale, MaxScale)
	}
	return nil
}

func checkMode(mode RoundingMode) error {
	if mode == RoundingUnspecified {
		return ErrRoundingUnspecified
	}
	if !mode.Valid() {
		return fmt.Errorf("%w: %d", ErrRoundingMode, uint8(mode))
	}
	return nil
}

// nonFiniteTokens are the spellings the backend would otherwise accept. They
// are rejected before parsing so the error names the real problem.
var nonFiniteTokens = map[string]struct{}{
	"nan": {}, "snan": {}, "qnan": {}, "inf": {}, "infinity": {},
}

// parseFixedText accepts only plain fixed-point text: an optional leading
// minus, one or more integer digits, and optionally a point followed by one or
// more fractional digits. Exponents, separators, whitespace and a leading plus
// are all rejected.
func parseFixedText(text string) (neg bool, intPart, fracPart string, err error) {
	body := text
	if rest, cut := strings.CutPrefix(body, "-"); cut {
		neg = true
		body = rest
	}
	if _, bad := nonFiniteTokens[strings.ToLower(body)]; bad {
		return false, "", "", fmt.Errorf("%w: %q", ErrNotFinite, text)
	}
	if body == "" {
		return false, "", "", fmt.Errorf("%w: %q is empty", ErrDecimalSyntax, text)
	}
	intPart, fracPart, hasPoint := strings.Cut(body, ".")
	if intPart == "" || (hasPoint && fracPart == "") {
		return false, "", "", fmt.Errorf("%w: %q", ErrDecimalSyntax, text)
	}
	for _, part := range []string{intPart, fracPart} {
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false, "", "", fmt.Errorf("%w: %q", ErrDecimalSyntax, text)
			}
		}
	}
	return neg, intPart, fracPart, nil
}

// Validate reports whether the decimal is usable.
func (d Decimal) Validate() error {
	if !d.set || d.unscaled == nil {
		return ErrDecimalUnset
	}
	if err := checkScale(d.scale); err != nil {
		return err
	}
	if err := checkMode(d.round); err != nil {
		return err
	}
	if d.unscaled.Sign() < 0 {
		return fmt.Errorf("values: internal: negative magnitude")
	}
	if d.neg && d.unscaled.Sign() == 0 {
		return ErrNegativeZero
	}
	if digits := len(d.unscaled.String()); digits > MaxPrecision {
		return fmt.Errorf("%w: %d digits", ErrPrecisionOverflow, digits)
	}
	return nil
}

// Scale returns the declared number of fractional digits.
func (d Decimal) Scale() int32 { return d.scale }

// Rounding returns the declared rounding mode.
func (d Decimal) Rounding() RoundingMode { return d.round }

// Unscaled returns a copy of the unscaled magnitude. The caller owns the copy;
// the decimal itself is immutable.
func (d Decimal) Unscaled() *big.Int {
	if d.unscaled == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(d.unscaled)
}

// Sign returns -1, 0 or +1.
func (d Decimal) Sign() int {
	if d.unscaled == nil || d.unscaled.Sign() == 0 {
		return 0
	}
	if d.neg {
		return -1
	}
	return 1
}

// IsZero reports whether the value is zero.
func (d Decimal) IsZero() bool { return d.Sign() == 0 }

// signed returns the signed unscaled integer.
func (d Decimal) signed() *big.Int {
	v := d.Unscaled()
	if d.neg {
		v.Neg(v)
	}
	return v
}

// WithRounding returns the same numeric value under a different declared
// rounding mode. It never changes the number.
func (d Decimal) WithRounding(mode RoundingMode) (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	if err := checkMode(mode); err != nil {
		return Decimal{}, err
	}
	out := d
	out.unscaled = d.Unscaled()
	out.round = mode
	return out, nil
}

// String returns the plain fixed-point text with exactly Scale fractional
// digits. It never uses exponent notation and never emits a negative zero.
func (d Decimal) String() string {
	if d.Validate() != nil {
		return ""
	}
	return formatFixed(d.unscaled, d.neg, d.scale)
}

func formatFixed(mag *big.Int, neg bool, scale int32) string {
	digits := mag.String()
	if int32(len(digits)) <= scale {
		digits = strings.Repeat("0", int(scale)-len(digits)+1) + digits
	}
	var b strings.Builder
	if neg && mag.Sign() != 0 {
		b.WriteByte('-')
	}
	split := int32(len(digits)) - scale
	b.WriteString(digits[:split])
	if scale > 0 {
		b.WriteByte('.')
		b.WriteString(digits[split:])
	}
	return b.String()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
//
// Layout: one sign byte (0x00 zero, 0x01 positive, 0x02 negative), the declared
// scale as a big-endian int32, the magnitude length as a big-endian uint32, and
// the minimal big-endian magnitude bytes. Zero has no magnitude bytes, so
// positive and negative zero cannot both exist.
func (d Decimal) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	mag := d.unscaled.Bytes()
	out := make([]byte, 0, 9+len(mag))
	switch d.Sign() {
	case 0:
		out = append(out, 0x00)
	case 1:
		out = append(out, 0x01)
	default:
		out = append(out, 0x02)
	}
	out = binary.BigEndian.AppendUint32(out, uint32(d.scale))
	out = binary.BigEndian.AppendUint32(out, uint32(len(mag)))
	return append(out, mag...)
}

// MarshalText implements encoding.TextMarshaler. The text carries the number
// and the declared rounding mode, because rounding is part of the value's
// contract and is not recoverable from the digits.
func (d Decimal) MarshalText() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return []byte(d.String() + "/" + d.round.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. The declared scale is the
// number of fractional digits written; the declared rounding mode follows the
// slash.
func (d *Decimal) UnmarshalText(text []byte) error {
	*d = Decimal{}
	number, modeToken, ok := strings.Cut(string(text), "/")
	if !ok {
		return fmt.Errorf("%w: decimal text needs a declared rounding mode", ErrDecimalSyntax)
	}
	mode, err := ParseRoundingMode(modeToken)
	if err != nil {
		return err
	}
	_, _, fracPart, err := parseFixedText(number)
	if err != nil {
		return err
	}
	parsed, err := NewDecimal(number, int32(len(fracPart)), mode)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Cmp compares two decimals numerically, independently of declared scale. It
// returns -1, 0 or +1, and 0 for an unset operand only when both are unset.
func (d Decimal) Cmp(o Decimal) int {
	if d.Validate() != nil || o.Validate() != nil {
		switch {
		case d.Validate() == nil:
			return 1
		case o.Validate() == nil:
			return -1
		default:
			return 0
		}
	}
	a, b := d.signed(), o.signed()
	switch {
	case d.scale < o.scale:
		a.Mul(a, pow10(o.scale-d.scale))
	case o.scale < d.scale:
		b.Mul(b, pow10(d.scale-o.scale))
	}
	return a.Cmp(b)
}

// Equal reports whether two decimals are numerically equal.
func (d Decimal) Equal(o Decimal) bool { return d.Cmp(o) == 0 }

func pow10(n int32) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// Add returns d+o. Both operands must declare the same scale: reconciling two
// different declared scales is a schema decision, not an arithmetic one. The
// result carries the receiver's declared rounding mode; addition at a shared
// scale is exact, so no rounding occurs.
func (d Decimal) Add(o Decimal) (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	if err := o.Validate(); err != nil {
		return Decimal{}, err
	}
	if d.scale != o.scale {
		return Decimal{}, fmt.Errorf("%w: %d vs %d", ErrScaleMismatch, d.scale, o.scale)
	}
	return fromSigned(new(big.Int).Add(d.signed(), o.signed()), d.scale, d.round)
}

// Sub returns d-o under the same rules as Add.
func (d Decimal) Sub(o Decimal) (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	if err := o.Validate(); err != nil {
		return Decimal{}, err
	}
	if d.scale != o.scale {
		return Decimal{}, fmt.Errorf("%w: %d vs %d", ErrScaleMismatch, d.scale, o.scale)
	}
	return fromSigned(new(big.Int).Sub(d.signed(), o.signed()), d.scale, d.round)
}

// Neg returns -d. Negating zero returns zero, never negative zero.
func (d Decimal) Neg() (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	return fromSigned(new(big.Int).Neg(d.signed()), d.scale, d.round)
}

// Abs returns |d|.
func (d Decimal) Abs() (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	return newDecimal(false, d.Unscaled(), d.scale, d.round)
}

// Mul returns d*o at an explicitly declared result scale and rounding mode. The
// exact product is computed first and rounded once.
func (d Decimal) Mul(o Decimal, scale int32, mode RoundingMode) (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	if err := o.Validate(); err != nil {
		return Decimal{}, err
	}
	product := new(big.Int).Mul(d.signed(), o.signed())
	return quantizeSigned(product, d.scale+o.scale, scale, mode)
}

// Div returns d/o at an explicitly declared result scale and rounding mode.
func (d Decimal) Div(o Decimal, scale int32, mode RoundingMode) (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	if err := o.Validate(); err != nil {
		return Decimal{}, err
	}
	if o.IsZero() {
		return Decimal{}, ErrDivideByZero
	}
	if err := checkScale(scale); err != nil {
		return Decimal{}, err
	}
	if err := checkMode(mode); err != nil {
		return Decimal{}, err
	}
	num, _, err := apd.NewFromString(d.String())
	if err != nil {
		return Decimal{}, fmt.Errorf("values: decimal backend rejected %q: %w", d.String(), err)
	}
	den, _, err := apd.NewFromString(o.String())
	if err != nil {
		return Decimal{}, fmt.Errorf("values: decimal backend rejected %q: %w", o.String(), err)
	}
	ctx := newDecimalContext(mode)
	var quotient apd.Decimal
	if _, err := ctx.Quo(&quotient, num, den); err != nil {
		return Decimal{}, fmt.Errorf("values: divide: %w", err)
	}
	if mode == RoundingExactRequired {
		var check apd.Decimal
		if _, err := ctx.Quantize(&check, &quotient, -scale); err != nil {
			return Decimal{}, fmt.Errorf("values: divide: %w", err)
		}
		if check.Cmp(&quotient) != 0 {
			return Decimal{}, fmt.Errorf("%w: %s / %s at scale %d", ErrInexact, d.String(), o.String(), scale)
		}
	}
	var out apd.Decimal
	if _, err := ctx.Quantize(&out, &quotient, -scale); err != nil {
		return Decimal{}, fmt.Errorf("values: divide: %w", err)
	}
	return decimalFromBackendText(out.Text('f'), scale, mode)
}

// Quantize returns the same number at a different declared scale, rounding by
// the given mode. RoundingExactRequired refuses to discard a nonzero digit.
func (d Decimal) Quantize(scale int32, mode RoundingMode) (Decimal, error) {
	if err := d.Validate(); err != nil {
		return Decimal{}, err
	}
	return quantizeSigned(d.signed(), d.scale, scale, mode)
}

// quantizeSigned rescales a signed unscaled integer. Widening the scale is
// exact; narrowing it defers the rounding decision to the qualified backend.
func quantizeSigned(signed *big.Int, fromScale, toScale int32, mode RoundingMode) (Decimal, error) {
	if err := checkScale(toScale); err != nil {
		return Decimal{}, err
	}
	if err := checkMode(mode); err != nil {
		return Decimal{}, err
	}
	if toScale >= fromScale {
		widened := new(big.Int).Mul(signed, pow10(toScale-fromScale))
		return fromSigned(widened, toScale, mode)
	}
	if mode == RoundingExactRequired {
		rem := new(big.Int).Mod(new(big.Int).Abs(signed), pow10(fromScale-toScale))
		if rem.Sign() != 0 {
			return Decimal{}, fmt.Errorf("%w: scale %d -> %d discards %s",
				ErrInexact, fromScale, toScale, rem.String())
		}
	}
	text := formatFixed(new(big.Int).Abs(signed), signed.Sign() < 0, fromScale)
	in, _, err := apd.NewFromString(text)
	if err != nil {
		return Decimal{}, fmt.Errorf("values: decimal backend rejected %q: %w", text, err)
	}
	ctx := newDecimalContext(mode)
	var out apd.Decimal
	if _, err := ctx.Quantize(&out, in, -toScale); err != nil {
		return Decimal{}, fmt.Errorf("values: quantize: %w", err)
	}
	return decimalFromBackendText(out.Text('f'), toScale, mode)
}

// decimalFromBackendText converts a backend result back into an owned Decimal,
// normalizing a negative-zero result to zero. No apd value ever leaves here.
func decimalFromBackendText(text string, scale int32, mode RoundingMode) (Decimal, error) {
	if stripped, cut := strings.CutPrefix(text, "-"); cut {
		if isAllZeroDigits(stripped) {
			text = stripped
		}
	}
	return NewDecimal(text, scale, mode)
}

func isAllZeroDigits(s string) bool {
	seen := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '.':
		case s[i] >= '0' && s[i] <= '9':
			seen = true
			if s[i] != '0' {
				return false
			}
		default:
			return false
		}
	}
	return seen
}
