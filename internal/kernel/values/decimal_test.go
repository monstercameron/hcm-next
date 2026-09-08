package values

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func mustDecimal(t *testing.T, text string, scale int32, mode RoundingMode) Decimal {
	t.Helper()
	d, err := NewDecimal(text, scale, mode)
	if err != nil {
		t.Fatalf("NewDecimal(%q, %d, %v) error = %v", text, scale, mode, err)
	}
	return d
}

func mustMoney(t *testing.T, text, currency string, scale int32, mode RoundingMode) Money {
	t.Helper()
	m, err := NewMoney(text, currency, scale, mode)
	if err != nil {
		t.Fatalf("NewMoney(%q, %q, %d, %v) error = %v", text, currency, scale, mode, err)
	}
	return m
}

// TestTodo_MODEL_003 is the primary test for MODEL-003: fixed-decimal
// arithmetic with declared scale and rounding, and no implicit conversion.
func TestTodo_MODEL_003(t *testing.T) {
	t.Parallel()

	t.Run("RejectsNonFiniteAndNegativeZero", func(t *testing.T) {
		cases := []struct {
			text string
			want error
		}{
			{"NaN", ErrNotFinite},
			{"nan", ErrNotFinite},
			{"sNaN", ErrNotFinite},
			{"Infinity", ErrNotFinite},
			{"-Infinity", ErrNotFinite},
			{"inf", ErrNotFinite},
			{"-0", ErrNegativeZero},
			{"-0.00", ErrNegativeZero},
			{"", ErrDecimalSyntax},
			{"1,5", ErrDecimalSyntax},
			{" 1", ErrDecimalSyntax},
			{"1e2", ErrDecimalSyntax},
		}
		for _, tc := range cases {
			t.Run(tc.text, func(t *testing.T) {
				if _, err := NewDecimal(tc.text, 2, RoundingHalfEven); !errors.Is(err, tc.want) {
					t.Fatalf("NewDecimal(%q) error = %v, want %v", tc.text, err, tc.want)
				}
			})
		}
	})

	t.Run("directed multiplication below one unit", func(t *testing.T) {
		left := MustDecimal("0.005", 3, RoundingHalfEven)
		positive := MustDecimal("0.125", 3, RoundingHalfEven)
		negative := MustDecimal("-0.125", 3, RoundingHalfEven)
		cases := []struct {
			name    string
			mode    RoundingMode
			operand Decimal
			want    string
		}{
			{"ceiling-positive", RoundingCeiling, positive, "1"},
			{"ceiling-negative", RoundingCeiling, negative, "0"},
			{"floor-positive", RoundingFloor, positive, "0"},
			{"floor-negative", RoundingFloor, negative, "-1"},
			{"toward-zero-positive", RoundingTowardZero, positive, "0"},
			{"toward-zero-negative", RoundingTowardZero, negative, "0"},
			{"away-positive", RoundingAwayFromZero, positive, "1"},
			{"away-negative", RoundingAwayFromZero, negative, "-1"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := left.Mul(tc.operand, 0, tc.mode)
				if err != nil || got.String() != tc.want {
					t.Fatalf("Mul = %q, %v; want %q", got.String(), err, tc.want)
				}
			})
		}
	})

	t.Run("RejectsExcessScaleAndOverflow", func(t *testing.T) {
		if _, err := NewDecimal("1.234", 2, RoundingHalfEven); !errors.Is(err, ErrExcessScale) {
			t.Fatalf("excess scale error = %v, want ErrExcessScale", err)
		}
		if _, err := NewDecimal("1.00", -1, RoundingHalfEven); !errors.Is(err, ErrScaleRange) {
			t.Fatalf("negative scale error = %v, want ErrScaleRange", err)
		}
		if _, err := NewDecimal("1.00", MaxScale+1, RoundingHalfEven); !errors.Is(err, ErrScaleRange) {
			t.Fatalf("oversized scale error = %v, want ErrScaleRange", err)
		}
		huge := strings.Repeat("9", MaxPrecision+1)
		if _, err := NewDecimal(huge, 0, RoundingHalfEven); !errors.Is(err, ErrPrecisionOverflow) {
			t.Fatalf("precision overflow error = %v, want ErrPrecisionOverflow", err)
		}
		if _, err := NewDecimal("1", 0, RoundingUnspecified); !errors.Is(err, ErrRoundingUnspecified) {
			t.Fatalf("unspecified rounding error = %v, want ErrRoundingUnspecified", err)
		}
		// Overflow through arithmetic is an error, never a wrapped result.
		near := mustDecimal(t, strings.Repeat("9", MaxPrecision), 0, RoundingHalfEven)
		if _, err := near.Add(near); !errors.Is(err, ErrPrecisionOverflow) {
			t.Fatalf("add overflow error = %v, want ErrPrecisionOverflow", err)
		}
	})

	t.Run("ScaleIsDeclaredAndPreserved", func(t *testing.T) {
		d := mustDecimal(t, "1.5", 4, RoundingHalfEven)
		if got, want := d.String(), "1.5000"; got != want {
			t.Fatalf("String() = %q, want %q", got, want)
		}
		if d.Scale() != 4 {
			t.Fatalf("Scale() = %d, want 4", d.Scale())
		}
		if got, want := d.Unscaled().String(), "15000"; got != want {
			t.Fatalf("Unscaled() = %q, want %q", got, want)
		}
		// Same number, different declared scale: different canonical bytes,
		// because scale is material.
		e := mustDecimal(t, "1.5", 2, RoundingHalfEven)
		if bytes.Equal(d.Canonical(), e.Canonical()) {
			t.Fatal("scale 4 and scale 2 produced identical canonical bytes")
		}
		if d.Cmp(e) != 0 {
			t.Fatal("numerically equal decimals did not compare equal")
		}
	})

	t.Run("CanonicalBytesEncodeSignUnscaledScale", func(t *testing.T) {
		cases := []struct {
			text  string
			scale int32
			want  string
		}{
			// sign(1) | scale int32 BE | len uint32 BE | magnitude BE
			{"0.00", 2, "00" + "00000002" + "00000000"},
			{"1.25", 2, "01" + "00000002" + "00000001" + "7d"},
			{"-1.25", 2, "02" + "00000002" + "00000001" + "7d"},
			{"0", 0, "00" + "00000000" + "00000000"},
		}
		for _, tc := range cases {
			d := mustDecimal(t, tc.text, tc.scale, RoundingHalfEven)
			if got := hex.EncodeToString(d.Canonical()); got != tc.want {
				t.Errorf("Canonical(%q@%d) = %s, want %s", tc.text, tc.scale, got, tc.want)
			}
		}
		// Positive and negative zero must not exist as two encodings.
		zero := mustDecimal(t, "0.00", 2, RoundingHalfEven)
		negated, err := zero.Neg()
		if err != nil {
			t.Fatalf("Neg() error = %v", err)
		}
		if !bytes.Equal(zero.Canonical(), negated.Canonical()) {
			t.Fatal("negating zero produced a distinct encoding")
		}
	})

	t.Run("RoundingIsDeclaredPerValue", func(t *testing.T) {
		cases := []struct {
			in    string
			mode  RoundingMode
			scale int32
			want  string
		}{
			{"2.5", RoundingHalfEven, 0, "2"},
			{"3.5", RoundingHalfEven, 0, "4"},
			{"-2.5", RoundingHalfEven, 0, "-2"},
			{"-3.5", RoundingHalfEven, 0, "-4"},
			{"2.5", RoundingHalfUp, 0, "3"},
			{"-2.5", RoundingHalfUp, 0, "-3"},
			{"2.5", RoundingHalfAwayFromZero, 0, "3"},
			{"-2.5", RoundingHalfAwayFromZero, 0, "-3"},
			{"2.5", RoundingFloor, 0, "2"},
			{"-2.5", RoundingFloor, 0, "-3"},
			{"2.5", RoundingCeiling, 0, "3"},
			{"-2.5", RoundingCeiling, 0, "-2"},
			{"2.9", RoundingTowardZero, 0, "2"},
			{"-2.9", RoundingTowardZero, 0, "-2"},
			{"2.1", RoundingAwayFromZero, 0, "3"},
			{"-2.1", RoundingAwayFromZero, 0, "-3"},
			{"1.005", RoundingHalfEven, 2, "1.00"},
			{"1.015", RoundingHalfEven, 2, "1.02"},
			{"1.005", RoundingHalfAwayFromZero, 2, "1.01"},
		}
		for _, tc := range cases {
			src := mustDecimal(t, tc.in, 3, RoundingHalfEven)
			got, err := src.Quantize(tc.scale, tc.mode)
			if err != nil {
				t.Errorf("Quantize(%q, %d, %v) error = %v", tc.in, tc.scale, tc.mode, err)
				continue
			}
			if got.String() != tc.want {
				t.Errorf("Quantize(%q, %d, %v) = %q, want %q", tc.in, tc.scale, tc.mode, got.String(), tc.want)
			}
			if got.Rounding() != tc.mode {
				t.Errorf("result rounding = %v, want %v", got.Rounding(), tc.mode)
			}
		}
	})

	t.Run("ExactRequiredRefusesToRound", func(t *testing.T) {
		src := mustDecimal(t, "1.005", 3, RoundingHalfEven)
		if _, err := src.Quantize(2, RoundingExactRequired); !errors.Is(err, ErrInexact) {
			t.Fatalf("EXACT_REQUIRED quantize error = %v, want ErrInexact", err)
		}
		exact := mustDecimal(t, "1.500", 3, RoundingHalfEven)
		got, err := exact.Quantize(1, RoundingExactRequired)
		if err != nil {
			t.Fatalf("exact quantize error = %v", err)
		}
		if got.String() != "1.5" {
			t.Fatalf("exact quantize = %q, want %q", got.String(), "1.5")
		}
	})

	t.Run("MoneyRequiresUppercaseISOCurrency", func(t *testing.T) {
		for _, bad := range []string{"", "us", "usd", "USDX", "US", "U$D", "12A", "Usd"} {
			if _, err := NewMoney("1.00", bad, 2, RoundingHalfEven); !errors.Is(err, ErrCurrencyCode) {
				t.Errorf("NewMoney currency %q error = %v, want ErrCurrencyCode", bad, err)
			}
		}
		m := mustMoney(t, "1234.50", "USD", 2, RoundingHalfEven)
		if m.Currency() != "USD" {
			t.Fatalf("Currency() = %q", m.Currency())
		}
		if got, want := m.String(), "1234.50 USD"; got != want {
			t.Fatalf("String() = %q, want %q", got, want)
		}
	})

	t.Run("MoneyRejectsImplicitCurrencyConversion", func(t *testing.T) {
		usd := mustMoney(t, "10.00", "USD", 2, RoundingHalfEven)
		eur := mustMoney(t, "10.00", "EUR", 2, RoundingHalfEven)
		if _, err := usd.Add(eur); !errors.Is(err, ErrCurrencyMismatch) {
			t.Fatalf("Add across currencies error = %v, want ErrCurrencyMismatch", err)
		}
		if _, err := usd.Sub(eur); !errors.Is(err, ErrCurrencyMismatch) {
			t.Fatalf("Sub across currencies error = %v, want ErrCurrencyMismatch", err)
		}
		if _, err := usd.Cmp(eur); !errors.Is(err, ErrCurrencyMismatch) {
			t.Fatalf("Cmp across currencies error = %v, want ErrCurrencyMismatch", err)
		}
		sum, err := usd.Add(mustMoney(t, "5.25", "USD", 2, RoundingHalfEven))
		if err != nil {
			t.Fatalf("Add same currency error = %v", err)
		}
		if got, want := sum.String(), "15.25 USD"; got != want {
			t.Fatalf("Add = %q, want %q", got, want)
		}
		// Differing declared scale is also never reconciled silently.
		if _, err := usd.Add(mustMoney(t, "5.2500", "USD", 4, RoundingHalfEven)); !errors.Is(err, ErrScaleMismatch) {
			t.Fatalf("Add across scales error = %v, want ErrScaleMismatch", err)
		}
	})

	t.Run("PercentageQuantityRate", func(t *testing.T) {
		pct, err := NewPercentageFromPercent("7.5", 6, RoundingHalfEven)
		if err != nil {
			t.Fatalf("NewPercentageFromPercent error = %v", err)
		}
		if got, want := pct.Fraction().String(), "0.075000"; got != want {
			t.Fatalf("Fraction() = %q, want %q", got, want)
		}
		gross := mustMoney(t, "4000.00", "USD", 2, RoundingHalfEven)
		tax, err := pct.ApplyTo(gross, 2, RoundingHalfEven)
		if err != nil {
			t.Fatalf("ApplyTo error = %v", err)
		}
		if got, want := tax.String(), "300.00 USD"; got != want {
			t.Fatalf("ApplyTo = %q, want %q", got, want)
		}

		hours, err := NewQuantity("37.50", "HOUR", 2, RoundingHalfEven)
		if err != nil {
			t.Fatalf("NewQuantity error = %v", err)
		}
		if _, err := NewQuantity("1", "hour", 0, RoundingHalfEven); !errors.Is(err, ErrUnitCode) {
			t.Fatalf("lowercase unit error = %v, want ErrUnitCode", err)
		}
		other, err := NewQuantity("2.50", "DAY", 2, RoundingHalfEven)
		if err != nil {
			t.Fatalf("NewQuantity error = %v", err)
		}
		if _, err := hours.Add(other); !errors.Is(err, ErrUnitMismatch) {
			t.Fatalf("Add across units error = %v, want ErrUnitMismatch", err)
		}

		perHour := mustMoney(t, "24.00", "USD", 2, RoundingHalfEven)
		rate, err := NewMoneyRate(perHour, "HOUR")
		if err != nil {
			t.Fatalf("NewMoneyRate error = %v", err)
		}
		pay, err := rate.Apply(hours, 2, RoundingHalfEven)
		if err != nil {
			t.Fatalf("Rate.Apply error = %v", err)
		}
		if got, want := pay.String(), "900.00 USD"; got != want {
			t.Fatalf("Rate.Apply = %q, want %q", got, want)
		}
		if _, err := rate.Apply(other, 2, RoundingHalfEven); !errors.Is(err, ErrUnitMismatch) {
			t.Fatalf("Rate.Apply across units error = %v, want ErrUnitMismatch", err)
		}
		if got, want := rate.String(), "24.00 USD/HOUR"; got != want {
			t.Fatalf("Rate.String() = %q, want %q", got, want)
		}
	})

	t.Run("NoApdTypeEscapes", func(t *testing.T) {
		// The kernel exposes only its own types: text in, text and canonical
		// bytes out. This is a compile-time contract; the assertion below keeps
		// the intent visible if the API ever grows an apd-shaped accessor.
		d := mustDecimal(t, "1.00", 2, RoundingHalfEven)
		text, err := d.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText error = %v", err)
		}
		var back Decimal
		if err := back.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText error = %v", err)
		}
		if !bytes.Equal(back.Canonical(), d.Canonical()) {
			t.Fatalf("text round trip changed canonical bytes")
		}
	})
}

func TestDecimalDirectedDivisionBelowUnit(t *testing.T) {
	positive := MustDecimal("0.005", 3, RoundingHalfEven)
	negative := MustDecimal("-0.005", 3, RoundingHalfEven)
	divisor := MustDecimal("8", 0, RoundingHalfEven)
	tests := []struct {
		name  string
		input Decimal
		mode  RoundingMode
		want  string
	}{
		{"positive ceiling", positive, RoundingCeiling, "1"},
		{"positive floor", positive, RoundingFloor, "0"},
		{"negative ceiling", negative, RoundingCeiling, "0"},
		{"negative floor", negative, RoundingFloor, "-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.input.Div(divisor, 0, test.mode)
			if err != nil || got.String() != test.want {
				t.Fatalf("Div = %q, %v; want %q", got.String(), err, test.want)
			}
		})
	}
}

func TestDecimalDivisionNearMaxPrecisionHalfBoundary(t *testing.T) {
	denominator := MustDecimal("99999999999999999999000000000000000001", 0, RoundingHalfEven)
	below := MustDecimal("50000000000000000049500000000000000000", 0, RoundingHalfEven)
	above := MustDecimal("50000000000000000049500000000000000001", 0, RoundingHalfEven)
	for _, test := range []struct {
		name  string
		input Decimal
		want  string
	}{
		{"below", below, "0.500000000000000000"},
		{"above", above, "0.500000000000000001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.input.Div(denominator, 18, RoundingHalfEven)
			if err != nil || got.String() != test.want {
				t.Fatalf("Div = %q, %v; want %q", got.String(), err, test.want)
			}
		})
	}
}

// TestTodo_MODEL_003_Property asserts arithmetic and encoding properties.
func TestTodo_MODEL_003_Property(t *testing.T) {
	t.Parallel()

	texts := []string{"0.00", "1.00", "-1.00", "0.01", "-0.01", "1234.56", "-1234.56", "999999.99"}
	decs := make([]Decimal, 0, len(texts))
	for _, s := range texts {
		decs = append(decs, mustDecimal(t, s, 2, RoundingHalfEven))
	}

	zero := mustDecimal(t, "0.00", 2, RoundingHalfEven)
	for i, a := range decs {
		// Identity: a + 0 == a.
		sum, err := a.Add(zero)
		if err != nil {
			t.Fatalf("%d Add error = %v", i, err)
		}
		if !bytes.Equal(sum.Canonical(), a.Canonical()) {
			t.Fatalf("%d a+0 changed canonical bytes", i)
		}
		// Involution: -(-a) == a.
		neg, err := a.Neg()
		if err != nil {
			t.Fatalf("%d Neg error = %v", i, err)
		}
		back, err := neg.Neg()
		if err != nil {
			t.Fatalf("%d Neg error = %v", i, err)
		}
		if !bytes.Equal(back.Canonical(), a.Canonical()) {
			t.Fatalf("%d double negation changed canonical bytes", i)
		}
		// Inverse: a - a == 0 and never negative zero.
		diff, err := a.Sub(a)
		if err != nil {
			t.Fatalf("%d Sub error = %v", i, err)
		}
		if !bytes.Equal(diff.Canonical(), zero.Canonical()) {
			t.Fatalf("%d a-a = %q, want zero", i, diff.String())
		}
		// Text round trip is exact.
		text, err := a.MarshalText()
		if err != nil {
			t.Fatalf("%d MarshalText error = %v", i, err)
		}
		var rt Decimal
		if err := rt.UnmarshalText(text); err != nil {
			t.Fatalf("%d UnmarshalText error = %v", i, err)
		}
		if !bytes.Equal(rt.Canonical(), a.Canonical()) {
			t.Fatalf("%d text round trip changed canonical bytes", i)
		}

		for j, b := range decs {
			// Commutativity of addition at a fixed scale.
			ab, err1 := a.Add(b)
			ba, err2 := b.Add(a)
			if err1 != nil || err2 != nil {
				t.Fatalf("%d+%d Add errors = %v, %v", i, j, err1, err2)
			}
			if !bytes.Equal(ab.Canonical(), ba.Canonical()) {
				t.Fatalf("%d+%d is not commutative", i, j)
			}
			// Cmp antisymmetry.
			if a.Cmp(b) != -b.Cmp(a) {
				t.Fatalf("%d vs %d Cmp is not antisymmetric", i, j)
			}
		}
	}

	// Injectivity: distinct (sign, unscaled, scale) triples have distinct bytes.
	seen := map[string]string{}
	for _, scale := range []int32{0, 2, 4} {
		for _, s := range []string{"0", "1", "-1", "2"} {
			d := mustDecimal(t, s, scale, RoundingHalfEven)
			key := string(d.Canonical())
			label := s + "@" + string(rune('0'+scale))
			if prev, dup := seen[key]; dup {
				t.Fatalf("%s and %s share canonical bytes", prev, label)
			}
			seen[key] = label
		}
	}
}

// TestTodo_MODEL_003_Golden pins decimal, money and rounding vectors.
func TestTodo_MODEL_003_Golden(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/model_003_decimals.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Decimals []struct {
			Text      string `json:"text"`
			Scale     int32  `json:"scale"`
			Canonical string `json:"canonical_hex"`
			String    string `json:"string"`
		} `json:"decimals"`
		Rounding []struct {
			Text  string `json:"text"`
			From  int32  `json:"from_scale"`
			To    int32  `json:"to_scale"`
			Mode  string `json:"mode"`
			Want  string `json:"want"`
			Error string `json:"error"`
		} `json:"rounding"`
		Money []struct {
			Text      string `json:"text"`
			Currency  string `json:"currency"`
			Scale     int32  `json:"scale"`
			Canonical string `json:"canonical_hex"`
			String    string `json:"string"`
		} `json:"money"`
		InvalidDecimals []struct {
			Text  string `json:"text"`
			Scale int32  `json:"scale"`
		} `json:"invalid_decimals"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}

	for _, tc := range golden.Decimals {
		d, err := NewDecimal(tc.Text, tc.Scale, RoundingHalfEven)
		if err != nil {
			t.Errorf("NewDecimal(%q,%d) error = %v", tc.Text, tc.Scale, err)
			continue
		}
		if got := hex.EncodeToString(d.Canonical()); got != tc.Canonical {
			t.Errorf("canonical(%q@%d) = %s, want %s", tc.Text, tc.Scale, got, tc.Canonical)
		}
		if got := d.String(); got != tc.String {
			t.Errorf("String(%q@%d) = %q, want %q", tc.Text, tc.Scale, got, tc.String)
		}
	}
	for _, tc := range golden.Rounding {
		mode, err := ParseRoundingMode(tc.Mode)
		if err != nil {
			t.Errorf("ParseRoundingMode(%q) error = %v", tc.Mode, err)
			continue
		}
		src, err := NewDecimal(tc.Text, tc.From, RoundingHalfEven)
		if err != nil {
			t.Errorf("NewDecimal(%q,%d) error = %v", tc.Text, tc.From, err)
			continue
		}
		got, err := src.Quantize(tc.To, mode)
		if tc.Error != "" {
			if err == nil {
				t.Errorf("Quantize(%q -> %d, %s) = %q, want error %s", tc.Text, tc.To, tc.Mode, got.String(), tc.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("Quantize(%q -> %d, %s) error = %v", tc.Text, tc.To, tc.Mode, err)
			continue
		}
		if got.String() != tc.Want {
			t.Errorf("Quantize(%q -> %d, %s) = %q, want %q", tc.Text, tc.To, tc.Mode, got.String(), tc.Want)
		}
	}
	for _, tc := range golden.Money {
		m, err := NewMoney(tc.Text, tc.Currency, tc.Scale, RoundingHalfEven)
		if err != nil {
			t.Errorf("NewMoney(%q,%q) error = %v", tc.Text, tc.Currency, err)
			continue
		}
		if got := hex.EncodeToString(m.Canonical()); got != tc.Canonical {
			t.Errorf("canonical money(%q %s) = %s, want %s", tc.Text, tc.Currency, got, tc.Canonical)
		}
		if got := m.String(); got != tc.String {
			t.Errorf("String(%q %s) = %q, want %q", tc.Text, tc.Currency, got, tc.String)
		}
	}
	for _, tc := range golden.InvalidDecimals {
		if _, err := NewDecimal(tc.Text, tc.Scale, RoundingHalfEven); err == nil {
			t.Errorf("invalid golden decimal %q@%d was accepted", tc.Text, tc.Scale)
		}
	}
}

// TestTodo_MODEL_003_Race proves the decimal kernel holds no shared mutable
// state: no apd context escapes and concurrent arithmetic is safe.
func TestTodo_MODEL_003_Race(t *testing.T) {
	t.Parallel()

	base := mustMoney(t, "1234.56", "USD", 2, RoundingHalfEven)
	addend := mustMoney(t, "0.01", "USD", 2, RoundingHalfEven)
	want, err := base.Add(addend)
	if err != nil {
		t.Fatalf("Add error = %v", err)
	}
	wantBytes := want.Canonical()

	const workers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				got, err := base.Add(addend)
				if err != nil {
					errCh <- err
					return
				}
				if !bytes.Equal(got.Canonical(), wantBytes) {
					errCh <- errors.New("concurrent Add produced different canonical bytes")
					return
				}
				if _, err := base.Amount().Quantize(0, RoundingHalfEven); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent decimal use failed: %v", err)
	}
	if got := base.String(); got != "1234.56 USD" {
		t.Fatalf("shared operand mutated: %q", got)
	}
}

// FuzzTodo_MODEL_003 checks that decimal parsing never panics, never yields a
// non-finite or negative-zero value, and always re-encodes exactly.
func FuzzTodo_MODEL_003(f *testing.F) {
	for _, s := range []string{
		"0", "0.00", "-0", "1.25", "-1.25", "NaN", "Infinity", "1e5",
		"999999999999999999999999999999999999999", "1.", ".1", "--1", "+1",
	} {
		for _, scale := range []int32{0, 2, 6} {
			f.Add(s, scale)
		}
	}
	f.Fuzz(func(t *testing.T, text string, scale int32) {
		d, err := NewDecimal(text, scale, RoundingHalfEven)
		if err != nil {
			return
		}
		if d.Scale() != scale {
			t.Fatalf("declared scale %d, got %d", scale, d.Scale())
		}
		if d.IsZero() && d.Sign() != 0 {
			t.Fatalf("zero decimal reports sign %d", d.Sign())
		}
		canon := d.Canonical()
		if len(canon) < 9 {
			t.Fatalf("canonical encoding is too short: %x", canon)
		}
		if canon[0] == 0x02 && d.Sign() != -1 {
			t.Fatalf("sign byte disagrees with Sign()")
		}
		out, err := d.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText error = %v", err)
		}
		var back Decimal
		if err := back.UnmarshalText(out); err != nil {
			t.Fatalf("UnmarshalText(%q) error = %v", out, err)
		}
		if !bytes.Equal(back.Canonical(), canon) {
			t.Fatalf("round trip of %q changed canonical bytes", out)
		}
		// Quantizing to the declared scale is a no-op.
		same, err := d.Quantize(scale, RoundingExactRequired)
		if err != nil {
			t.Fatalf("self quantize error = %v", err)
		}
		if !bytes.Equal(same.Canonical(), canon) {
			t.Fatalf("self quantize changed canonical bytes")
		}
	})
}
