package payband

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	testScale    int32 = 2
	testRounding       = values.RoundingHalfEven
)

// money builds a test amount at the fixture money contract.
func money(t *testing.T, text string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, "USD", testScale, testRounding)
	if err != nil {
		t.Fatalf("money(%q): %v", text, err)
	}
	return m
}

// testBand is the OPS-HRBP3 band: 92,000 / 112,000 / 132,000 USD. The interior
// width of 40,000 makes every quartile boundary an exact cent, so a boundary
// assertion is testing the cut rule rather than a rounding artifact.
func testBand(t *testing.T) Band {
	t.Helper()
	return Band{
		ID:       "BAND-OPS-P3-USEAST",
		Version:  "2026.1",
		Scope:    Scope{JobCode: "OPS-HRBP3", Grade: "P3", PayZone: "US-EAST"},
		Minimum:  money(t, "92000.00"),
		Midpoint: money(t, "112000.00"),
		Maximum:  money(t, "132000.00"),
	}
}

func TestEvaluatePayBandPositionComputesExactCompaRatioAndPenetration(t *testing.T) {
	band := testBand(t)

	cases := []struct {
		name        string
		amount      string
		placement   Placement
		compaRatio  string
		penetration string
		quartile    int
		toMinimum   string
		toMaximum   string
	}{
		{"at midpoint", "112000.00", PlacementInBand, "1.0000", "0.5000", 3, "20000.00", "20000.00"},
		{"at minimum", "92000.00", PlacementInBand, "0.8214", "0.0000", 1, "0.00", "40000.00"},
		{"at maximum", "132000.00", PlacementInBand, "1.1786", "1.0000", 4, "40000.00", "0.00"},
		{"first quartile boundary", "102000.00", PlacementInBand, "0.9107", "0.2500", 2, "10000.00", "30000.00"},
		{"third quartile boundary", "122000.00", PlacementInBand, "1.0893", "0.7500", 4, "30000.00", "10000.00"},
		{"below minimum", "85000.00", PlacementBelowMinimum, "0.7589", "-0.1750", 0, "-7000.00", "47000.00"},
		{"above maximum", "140000.00", PlacementAboveMaximum, "1.2500", "1.2000", 0, "48000.00", "-8000.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			position, err := Evaluate(band, money(t, tc.amount))
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if position.Placement != tc.placement {
				t.Errorf("placement = %s, want %s", position.Placement, tc.placement)
			}
			if got := position.CompaRatio.String(); got != tc.compaRatio {
				t.Errorf("compa ratio = %s, want %s", got, tc.compaRatio)
			}
			if got := position.RangePenetration.String(); got != tc.penetration {
				t.Errorf("range penetration = %s, want %s", got, tc.penetration)
			}
			if position.Quartile != tc.quartile {
				t.Errorf("quartile = %d, want %d", position.Quartile, tc.quartile)
			}
			if got := position.DistanceToMinimum.Amount().String(); got != tc.toMinimum {
				t.Errorf("distance to minimum = %s, want %s", got, tc.toMinimum)
			}
			if got := position.DistanceToMaximum.Amount().String(); got != tc.toMaximum {
				t.Errorf("distance to maximum = %s, want %s", got, tc.toMaximum)
			}
			if position.BandID != band.ID || position.BandVersion != band.Version {
				t.Errorf("position cites %s@%s, want %s@%s",
					position.BandID, position.BandVersion, band.ID, band.Version)
			}
			if position.CompaRatio.Scale() != RatioScale || position.RangePenetration.Scale() != RatioScale {
				t.Errorf("ratios must be declared at scale %d, got %d and %d",
					RatioScale, position.CompaRatio.Scale(), position.RangePenetration.Scale())
			}
		})
	}
}

func TestEvaluatePayBandPositionIsReproducibleByteForByte(t *testing.T) {
	band := testBand(t)
	amount := money(t, "117500.00")

	first, err := Evaluate(band, amount)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	second, err := Evaluate(band, amount)
	if err != nil {
		t.Fatalf("Evaluate (repeat): %v", err)
	}
	if !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("two evaluations of identical inputs produced different canonical bytes")
	}
	if first.Canonical() == nil {
		t.Fatal("a valid position must have a canonical encoding")
	}
}

func TestEvaluatePayBandPositionRejectsUndefinedInputs(t *testing.T) {
	base := testBand(t)

	otherCurrency, err := values.NewMoney("100000.00", "EUR", testScale, testRounding)
	if err != nil {
		t.Fatalf("euro amount: %v", err)
	}
	wideScale, err := values.NewMoney("100000.0000", "USD", 4, testRounding)
	if err != nil {
		t.Fatalf("wide-scale amount: %v", err)
	}

	degenerate := base
	degenerate.Minimum = money(t, "112000.00")
	degenerate.Maximum = money(t, "112000.00")

	zeroMidpoint := base
	zeroMidpoint.Minimum = money(t, "0.00")
	zeroMidpoint.Midpoint = money(t, "0.00")

	unordered := base
	unordered.Midpoint = money(t, "200000.00")

	unversioned := base
	unversioned.Version = ""

	unscoped := base
	unscoped.Scope.Grade = ""

	cases := []struct {
		name   string
		band   Band
		amount values.Money
		want   error
	}{
		{"amount in another currency", base, otherCurrency, ErrAmountCurrency},
		{"amount at another declared scale", base, wideScale, ErrScaleMismatch},
		{"minimum equals maximum", degenerate, money(t, "112000.00"), ErrBandDegenerate},
		{"zero midpoint", zeroMidpoint, money(t, "50000.00"), ErrBandMidpointZero},
		{"midpoint above maximum", unordered, money(t, "100000.00"), ErrBandOrder},
		{"band without a version", unversioned, money(t, "100000.00"), ErrBandIdentity},
		{"band without a full scope", unscoped, money(t, "100000.00"), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			position, err := Evaluate(tc.band, tc.amount)
			if err == nil {
				t.Fatalf("Evaluate returned a position for an undefined input: %+v", position)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestEvaluatePayBandPositionRefusesCanonicalBytesForAnInvalidBand(t *testing.T) {
	band := testBand(t)
	band.ID = ""
	if band.Canonical() != nil {
		t.Fatal("an invalid band must have no canonical encoding")
	}
	if band.String() != "" {
		t.Fatal("an invalid band must not render a human form that looks authoritative")
	}
}

// FuzzEvaluatePayBandPosition drives the band arithmetic with arbitrary bounds
// and amounts. It asserts the properties that must hold for every input rather
// than specific numbers: the engine never panics, the placement is exactly the
// comparison against the bounds, the distances are exact subtractions, and two
// evaluations of the same input are byte-identical.
func FuzzEvaluatePayBandPosition(f *testing.F) {
	f.Add(int64(9200000), int64(2000000), int64(2000000), int64(11200000))
	f.Add(int64(0), int64(1), int64(1), int64(0))
	f.Add(int64(100), int64(0), int64(1), int64(-5000))
	f.Add(int64(1), int64(999999999), int64(999999999), int64(123456789))

	f.Fuzz(func(t *testing.T, minCents, lowerSpan, upperSpan, amountCents int64) {
		const limit = 1_000_000_000_000
		minC := abs64(minCents) % limit
		midC := minC + abs64(lowerSpan)%limit
		maxC := midC + abs64(upperSpan)%limit
		if maxC == minC || midC == 0 {
			t.Skip("degenerate range or zero midpoint is a rejected contract, not a result")
		}
		amountC := amountCents % (limit * 4)

		band := Band{
			ID:       "FUZZ-BAND",
			Version:  "1",
			Scope:    Scope{JobCode: "J", Grade: "G", PayZone: "Z"},
			Minimum:  fuzzMoney(t, minC),
			Midpoint: fuzzMoney(t, midC),
			Maximum:  fuzzMoney(t, maxC),
		}
		amount := fuzzMoney(t, amountC)

		position, err := Evaluate(band, amount)
		if err != nil {
			if band.Validate() == nil {
				t.Fatalf("Evaluate failed on a valid band: %v", err)
			}
			return
		}

		wantPlacement := PlacementInBand
		switch {
		case amountC < minC:
			wantPlacement = PlacementBelowMinimum
		case amountC > maxC:
			wantPlacement = PlacementAboveMaximum
		}
		if position.Placement != wantPlacement {
			t.Fatalf("placement = %s, want %s for amount %d in [%d,%d]",
				position.Placement, wantPlacement, amountC, minC, maxC)
		}

		if position.Placement == PlacementInBand {
			if position.Quartile < 1 || position.Quartile > 4 {
				t.Fatalf("in-band quartile = %d, want 1..4", position.Quartile)
			}
		} else if position.Quartile != 0 {
			t.Fatalf("out-of-band quartile = %d, want 0", position.Quartile)
		}

		assertCents(t, "distance to minimum", position.DistanceToMinimum, amountC-minC)
		assertCents(t, "distance to maximum", position.DistanceToMaximum, maxC-amountC)

		repeat, err := Evaluate(band, amount)
		if err != nil {
			t.Fatalf("repeat evaluation failed: %v", err)
		}
		if !bytes.Equal(position.Canonical(), repeat.Canonical()) {
			t.Fatal("identical inputs produced different canonical bytes")
		}
	})
}

// abs64 returns the magnitude of v without overflowing on math.MinInt64.
func abs64(v int64) int64 {
	if v < 0 {
		if v == -1<<63 {
			return 1 << 62
		}
		return -v
	}
	return v
}

// fuzzMoney builds a USD amount from a signed cent count.
func fuzzMoney(t *testing.T, cents int64) values.Money {
	t.Helper()
	text := centsText(cents)
	m, err := values.NewMoney(text, "USD", testScale, testRounding)
	if err != nil {
		t.Fatalf("fuzzMoney(%d -> %q): %v", cents, text, err)
	}
	return m
}

// centsText renders a signed cent count as fixed-point text at scale 2.
func centsText(cents int64) string {
	neg := cents < 0
	mag := big.NewInt(cents)
	if neg {
		mag.Neg(mag)
	}
	whole := new(big.Int)
	frac := new(big.Int)
	whole.DivMod(mag, big.NewInt(100), frac)
	out := whole.String() + "." + pad2(frac.Int64())
	if neg && cents != 0 {
		out = "-" + out
	}
	return out
}

// pad2 renders 0..99 with a leading zero.
func pad2(v int64) string {
	digits := []byte{byte('0' + v/10), byte('0' + v%10)}
	return string(digits)
}

// assertCents checks that a money result equals an exact cent count.
func assertCents(t *testing.T, label string, got values.Money, wantCents int64) {
	t.Helper()
	want := centsText(wantCents)
	if got.Amount().String() != want {
		t.Fatalf("%s = %s, want %s", label, got.Amount(), want)
	}
}
