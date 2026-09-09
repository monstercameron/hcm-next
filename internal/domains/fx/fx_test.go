package fx

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func fxInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

func fxInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	first := fxInstant(t, start)
	if end == "" {
		interval, err := values.NewOpenInstantInterval(first)
		if err != nil {
			t.Fatal(err)
		}
		return interval
	}
	interval, err := values.NewInstantInterval(first, fxInstant(t, end))
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func fxRate(t *testing.T, text string) values.Decimal {
	t.Helper()
	rate, err := values.NewDecimal(text, 6, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return rate
}

func validFXSource(t *testing.T, id string) RateSourceRevision {
	t.Helper()
	pair, err := NewCurrencyPair("USD", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewRateSourceRevision(RateSourceRevision{SourceID: id, Revision: 1, ProviderRef: "provider:" + id, Pairs: []CurrencyPair{pair}, QuoteCadence: CadenceHourly, AuthorityClass: AuthorityPrimary, Effective: fxInterval(t, "2026-01-01T00:00:00Z", "")})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func validFXProfile(t *testing.T, sources ...string) ConversionProfileRevision {
	t.Helper()
	profile, err := NewConversionProfileRevision(ConversionProfileRevision{ProfileID: "payroll-fx", Revision: 1, RoundingRule: RoundingRule{Scale: 2, Mode: values.RoundingHalfEven}, Tolerance: 2 * time.Hour, FallbackSourceOrder: sources, Effective: fxInterval(t, "2026-01-01T00:00:00Z", "")})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func validFXQuote(t *testing.T, id, source string, asOf string) FXQuoteRevision {
	t.Helper()
	quote, err := NewFXQuoteRevision(FXQuoteRevision{QuoteID: id, SourceID: source, SourceRevision: 1, BaseCurrency: "USD", QuoteCurrency: "EUR", Rate: fxRate(t, "0.923456"), AsOf: fxInstant(t, asOf), KnownAt: fxInstant(t, "2026-06-01T11:00:00Z"), MarketConvention: ConventionSpot, Confidence: ConfidenceHigh})
	if err != nil {
		t.Fatal(err)
	}
	return quote
}

// TestFXQuoteResolutionRequiresSourceEffectiveKnownTimeAndPair is the
// primary FX-001 acceptance case: selection is explicit, fresh, and digested.
func TestFXQuoteResolutionRequiresSourceEffectiveKnownTimeAndPair(t *testing.T) {
	source := validFXSource(t, "source-primary")
	profile := validFXProfile(t, source.SourceID)
	quote := validFXQuote(t, "quote-1", source.SourceID, "2026-06-01T10:00:00Z")
	result, err := ResolveQuote(QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{quote}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResolutionQuote || result.Quote.QuoteID != quote.QuoteID || result.SourcePriority != 1 || result.CanonicalDigest == "" {
		t.Fatalf("resolution = %+v", result)
	}
	if _, err := result.Explain(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_FX_001_Property(t *testing.T) {
	source := validFXSource(t, "source-property")
	profile := validFXProfile(t, source.SourceID)
	one := validFXQuote(t, "quote-a", source.SourceID, "2026-06-01T10:00:00Z")
	two := validFXQuote(t, "quote-b", source.SourceID, "2026-06-01T10:00:00Z")
	result, err := ResolveQuote(QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{two, one}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ResolutionConflict {
		t.Fatalf("duplicate observations = %s", result.Status)
	}
}

func TestTodo_FX_001_Golden(t *testing.T) {
	source := validFXSource(t, "source-golden")
	profile := validFXProfile(t, source.SourceID)
	quote := validFXQuote(t, "quote-golden", source.SourceID, "2026-06-01T10:00:00Z")
	request := QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{quote}}
	one, err := ResolveQuote(request)
	if err != nil {
		t.Fatal(err)
	}
	two, err := ResolveQuote(request)
	if err != nil {
		t.Fatal(err)
	}
	if one.CanonicalDigest == "" || one.CanonicalDigest != two.CanonicalDigest {
		t.Fatal("resolution digest is not stable")
	}
}

func TestTodo_FX_001_Race(t *testing.T) {
	source := validFXSource(t, "source-race")
	profile := validFXProfile(t, source.SourceID)
	quote := validFXQuote(t, "quote-race", source.SourceID, "2026-06-01T10:00:00Z")
	request := QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{quote}}
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() { defer wait.Done(); _, _ = ResolveQuote(request) }()
	}
	wait.Wait()
}

func TestTodo_FX_001_Fault(t *testing.T) {
	source := validFXSource(t, "source-fault")
	profile := validFXProfile(t, source.SourceID)
	missing := validFXQuote(t, "quote-missing-pair", source.SourceID, "2026-06-01T10:00:00Z")
	missing.BaseCurrency = ""
	missing.CanonicalDigest = ""
	if _, err := ResolveQuote(QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{missing}}); !errors.Is(err, ErrInvalidQuote) {
		t.Fatalf("invalid pair quote error = %v", err)
	}
	invalid := validFXQuote(t, "quote-no-known", source.SourceID, "2026-06-01T10:00:00Z")
	invalid.KnownAt = values.Instant{}
	if _, err := ResolveQuote(QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{invalid}}); !errors.Is(err, ErrInvalidQuote) {
		t.Fatalf("missing known_at error = %v", err)
	}
}

func TestTodo_FX_001_Security(t *testing.T) {
	source := validFXSource(t, "source-security")
	profile := validFXProfile(t, source.SourceID)
	quote := validFXQuote(t, "quote-security", source.SourceID, "2026-06-01T10:00:00Z")
	result, err := ResolveQuote(QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{quote}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := result.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, quote.Rate.String()) {
		t.Fatalf("rate leaked into explanation: %q", text)
	}
}

func TestTodo_FX_001_Conformance(t *testing.T) {
	if Version() <= 0 || !CadenceDaily.Valid() || !AuthorityPrimary.Valid() || !ConventionOfficial.Valid() || !ConfidenceMedium.Valid() {
		t.Fatal("closed vocabulary contract failed")
	}
	if QuoteCadence("WEEKLY").Valid() || AuthorityClass("UNTRUSTED").Valid() {
		t.Fatal("unknown vocabulary accepted")
	}
}

func TestTodo_FX_001_Mutation(t *testing.T) {
	source := validFXSource(t, "source-mutation")
	profile := validFXProfile(t, source.SourceID)
	quote := validFXQuote(t, "quote-mutation", source.SourceID, "2026-06-01T10:00:00Z")
	before := quote.CanonicalDigest
	quote.Rate = fxRate(t, "0.900000")
	if quote.CanonicalDigest != before {
		t.Fatal("caller mutation changed the original digest")
	}
	if err := quote.Validate(); err == nil {
		t.Fatal("mutated quote retained a valid stale digest")
	}
	if profile.CanonicalDigest == "" || source.CanonicalDigest == "" {
		t.Fatal("missing immutable digests")
	}
}

func fxMoney(t *testing.T, text, currency string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, currency, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func fxPairSource(t *testing.T, id, base, quote string) RateSourceRevision {
	t.Helper()
	p, err := NewCurrencyPair(base, quote)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewRateSourceRevision(RateSourceRevision{SourceID: id, Revision: 1, ProviderRef: "provider:" + id, Pairs: []CurrencyPair{p}, QuoteCadence: CadenceHourly, AuthorityClass: AuthorityPrimary, Effective: fxInterval(t, "2026-01-01T00:00:00Z", "")})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fxPairQuote(t *testing.T, id string, source RateSourceRevision, base, quote, rate string) FXQuoteRevision {
	t.Helper()
	q, err := NewFXQuoteRevision(FXQuoteRevision{QuoteID: id, SourceID: source.SourceID, SourceRevision: 1, BaseCurrency: base, QuoteCurrency: quote, Rate: fxRate(t, rate), AsOf: fxInstant(t, "2026-06-01T10:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T11:00:00Z"), MarketConvention: ConventionSpot, Confidence: ConfidenceHigh})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// TestFXConversionReturnsExactAmountRoundingAndTrace proves direct, inverse,
// and triangulated paths use fixed decimal arithmetic and retain quote trace.
func TestFXConversionReturnsExactAmountRoundingAndTrace(t *testing.T) {
	directSource := fxPairSource(t, "direct", "USD", "EUR")
	directQuote := fxPairQuote(t, "direct-q", directSource, "USD", "EUR", "0.923456")
	profile := validFXProfile(t, directSource.SourceID)
	base := MoneyConversionRequest{Amount: fxMoney(t, "100.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: profile, Sources: []RateSourceRevision{directSource}, Quotes: []FXQuoteRevision{directQuote}}
	got, err := ConvertMoney(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != PathDirect || got.Converted.String() != "92.35 EUR" || len(got.Legs) != 1 || got.Legs[0].QuoteDigest != directQuote.CanonicalDigest {
		t.Fatalf("direct result = %+v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}

	inverseSource := fxPairSource(t, "inverse", "USD", "EUR")
	inverseQuote := fxPairQuote(t, "inverse-q", inverseSource, "USD", "EUR", "0.923456")
	inverseProfile := validFXProfile(t, inverseSource.SourceID)
	inverse := base
	inverse.Amount = fxMoney(t, "100.00", "EUR")
	inverse.TargetCurrency = "USD"
	inverse.Profile = inverseProfile
	inverse.Sources = []RateSourceRevision{inverseSource}
	inverse.Quotes = []FXQuoteRevision{inverseQuote}
	inv, err := ConvertMoney(inverse)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Path != PathInverse || inv.Converted.String() != "108.29 USD" || inv.Legs[0].Direction != PathInverse {
		t.Fatalf("inverse result = %+v", inv)
	}

	one := fxPairSource(t, "tri-one", "USD", "GBP")
	two := fxPairSource(t, "tri-two", "GBP", "EUR")
	q1 := fxPairQuote(t, "tri-q1", one, "USD", "GBP", "0.800000")
	q2 := fxPairQuote(t, "tri-q2", two, "GBP", "EUR", "1.100000")
	triProfile := validFXProfile(t, one.SourceID, two.SourceID)
	triProfile.TriangulationCurrencies = []string{"GBP"}
	triProfile, err = NewConversionProfileRevision(triProfile)
	if err != nil {
		t.Fatal(err)
	}
	tri := base
	tri.Profile = triProfile
	tri.ViaCurrency = "GBP"
	tri.Sources = []RateSourceRevision{one, two}
	tri.Quotes = []FXQuoteRevision{q1, q2}
	tr, err := ConvertMoney(tri)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Path != PathTriangulated || tr.Converted.String() != "88.00 EUR" || len(tr.Legs) != 2 {
		t.Fatalf("triangulated result = %+v", tr)
	}
}

func ratRoundedText(value *big.Rat, scale int32, mode values.RoundingMode) (string, bool) {
	negative := value.Sign() < 0
	scaled := new(big.Rat).Mul(new(big.Rat).Abs(value), new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(scaled.Num(), scaled.Denom(), remainder)
	exact := remainder.Sign() == 0
	increment := false
	if !exact {
		switch mode {
		case values.RoundingFloor:
			increment = negative
		case values.RoundingCeiling:
			increment = !negative
		case values.RoundingAwayFromZero:
			increment = true
		case values.RoundingHalfEven, values.RoundingHalfUp, values.RoundingHalfAwayFromZero:
			comparison := new(big.Int).Lsh(new(big.Int).Set(remainder), 1).Cmp(scaled.Denom())
			increment = comparison > 0 || comparison == 0 && (mode != values.RoundingHalfEven || quotient.Bit(0) == 1)
		}
	}
	if increment {
		quotient.Add(quotient, big.NewInt(1))
	}
	digits := quotient.String()
	for int32(len(digits)) <= scale {
		digits = "0" + digits
	}
	if scale > 0 {
		digits = digits[:len(digits)-int(scale)] + "." + digits[len(digits)-int(scale):]
	}
	if negative && quotient.Sign() != 0 {
		digits = "-" + digits
	}
	return digits, exact
}

func TestTodo_FX_002_Property(t *testing.T) {
	operands := []string{"0.005", "-0.005", "1.25", "-7.75", "8", "10", "999.999", "-999.999"}
	rates := []string{"0.125", "0.5", "1.25", "2", "3.2", "7.75"}
	scales := []int32{0, 2, 4}
	modes := []values.RoundingMode{values.RoundingHalfEven, values.RoundingHalfUp, values.RoundingHalfAwayFromZero, values.RoundingFloor, values.RoundingCeiling, values.RoundingTowardZero, values.RoundingAwayFromZero, values.RoundingExactRequired}
	asOf := fxInstant(t, "2026-06-01T12:00:00Z")
	caseNumber := 0
	for _, operandText := range operands {
		for _, rateText := range rates {
			caseNumber++
			source := fxPairSource(t, fmt.Sprintf("property-%d", caseNumber), "USD", "EUR")
			rateScale := int32(0)
			if point := strings.IndexByte(rateText, '.'); point >= 0 {
				rateScale = int32(len(rateText) - point - 1)
			}
			rate, err := values.NewDecimal(rateText, rateScale, values.RoundingHalfEven)
			if err != nil {
				t.Fatal(err)
			}
			quote := fxPairQuote(t, fmt.Sprintf("property-q-%d", caseNumber), source, "USD", "EUR", "1.000000")
			quote.Rate = rate
			quote, err = NewFXQuoteRevision(quote)
			if err != nil {
				t.Fatal(err)
			}
			operandScale := int32(0)
			if point := strings.IndexByte(operandText, '.'); point >= 0 {
				operandScale = int32(len(operandText) - point - 1)
			}
			operandRat, _ := new(big.Rat).SetString(operandText)
			rateRat, _ := new(big.Rat).SetString(rateText)
			for _, scale := range scales {
				for _, mode := range modes {
					profile := validFXProfile(t, source.SourceID)
					profile.RoundingRule = RoundingRule{Scale: scale, Mode: mode}
					profile, err = NewConversionProfileRevision(profile)
					if err != nil {
						t.Fatal(err)
					}
					for _, inverse := range []bool{false, true} {
						from, to := "USD", "EUR"
						exactValue := new(big.Rat).Mul(operandRat, rateRat)
						if inverse {
							from, to = "EUR", "USD"
							exactValue.Quo(operandRat, rateRat)
						}
						want, exactlyRepresentable := ratRoundedText(exactValue, scale, mode)
						amount, err := values.NewMoney(operandText, from, operandScale, mode)
						if err != nil {
							t.Fatal(err)
						}
						got, err := ConvertMoney(MoneyConversionRequest{Amount: amount, TargetCurrency: to, AsOf: asOf, KnownAt: asOf, Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{quote}})
						if mode == values.RoundingExactRequired && !exactlyRepresentable {
							if !errors.Is(err, ErrInvalidConversion) {
								t.Fatalf("inexact conversion was not refused: operand=%s rate=%s scale=%d inverse=%v err=%v", operandText, rateText, scale, inverse, err)
							}
							continue
						}
						if err != nil || got.Converted.Amount().String() != want {
							t.Fatalf("operand=%s rate=%s scale=%d mode=%s inverse=%v: got=%s want=%s err=%v", operandText, rateText, scale, mode, inverse, got.Converted.String(), want, err)
						}
						if exactlyRepresentable && !got.Remainder.IsZero() {
							t.Fatalf("exact conversion retained residual: operand=%s rate=%s scale=%d inverse=%v remainder=%s", operandText, rateText, scale, inverse, got.Remainder.String())
						}
					}
				}
			}
		}
	}

	adversarial := []struct {
		amount, rate string
		scale        int32
	}{
		{"49999999999999999999999999999999999999", "99999999999999999999999999999999999999", 0},
		{"50000000000000000000000000000000000000", "99999999999999999999999999999999999999", 0},
		{"50000000000000000049500000000000000000", "99999999999999999999000000000000000001", 18},
		{"50000000000000000049500000000000000001", "99999999999999999999000000000000000001", 18},
	}
	for i, test := range adversarial {
		for _, sign := range []string{"", "-"} {
			source := fxPairSource(t, fmt.Sprintf("boundary-%d-%t", i, sign == "-"), "USD", "EUR")
			rate, err := values.NewDecimal(test.rate, 0, values.RoundingHalfEven)
			if err != nil {
				t.Fatal(err)
			}
			quote := fxPairQuote(t, fmt.Sprintf("boundary-q-%d-%t", i, sign == "-"), source, "USD", "EUR", "1.000000")
			quote.Rate = rate
			quote, err = NewFXQuoteRevision(quote)
			if err != nil {
				t.Fatal(err)
			}
			amountText := sign + test.amount
			amountRat, _ := new(big.Rat).SetString(amountText)
			rateRat, _ := new(big.Rat).SetString(test.rate)
			exactValue := new(big.Rat).Quo(amountRat, rateRat)
			for _, mode := range modes {
				profile := validFXProfile(t, source.SourceID)
				profile.RoundingRule = RoundingRule{Scale: test.scale, Mode: mode}
				profile, err = NewConversionProfileRevision(profile)
				if err != nil {
					t.Fatal(err)
				}
				want, exact := ratRoundedText(exactValue, test.scale, mode)
				amount, err := values.NewMoney(amountText, "EUR", 0, mode)
				if err != nil {
					t.Fatal(err)
				}
				got, err := ConvertMoney(MoneyConversionRequest{Amount: amount, TargetCurrency: "USD", AsOf: asOf, KnownAt: asOf, Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{quote}})
				if mode == values.RoundingExactRequired && !exact {
					if !errors.Is(err, ErrInvalidConversion) {
						t.Fatalf("adversarial inexact conversion was not refused: amount=%s rate=%s scale=%d err=%v", amountText, test.rate, test.scale, err)
					}
					continue
				}
				if err != nil || got.Converted.Amount().String() != want {
					t.Fatalf("adversarial amount=%s rate=%s scale=%d mode=%s: got=%s want=%s err=%v", amountText, test.rate, test.scale, mode, got.Converted.String(), want, err)
				}
			}
		}
	}
}

func TestTodo_FX_002_Golden(t *testing.T) {
	s := fxPairSource(t, "golden-002", "USD", "EUR")
	q := fxPairQuote(t, "golden-q", s, "USD", "EUR", "0.923456")
	p := validFXProfile(t, s.SourceID)
	req := MoneyConversionRequest{Amount: fxMoney(t, "100.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}}
	a, err := ConvertMoney(req)
	if err != nil {
		t.Fatal(err)
	}
	want := "0724736368656d612868636d6e6578742e646f6d61696e732e66782e4d6f6e6579436f6e76657273696f6e526573756c740f24736368656d615f76657273696f6e010206736f757263650e010000000200000002271055534409636f6e7665727465640e01000000020000000224134555520470617468064449524543540972656d61696e646572100200000012000000070fa1c6d50300000a70726f66696c655f69640a706179726f6c6c2d66781070726f66696c655f7265766973696f6e01020d726f756e64696e675f72756c65510724736368656d611f68636d6e6578742e646f6d61696e732e66782e526f756e64696e6752756c650f24736368656d615f76657273696f6e0102057363616c650104046d6f64650948414c465f4556454e056c6567732301020466726f6d0355534402746f034555520871756f74655f696408676f6c64656e2d710c71756f74655f646967657374477368613235363a3030306537633733313365316431373232353936636664666234373464396137626534323032653836656139393963313865393165643666313264306237346109736f757263655f69640a676f6c64656e2d3030320f736f757263655f7265766973696f6e010204726174650c0100000006000000030e174009646972656374696f6e0644495245435405696e7075740e0100000002000000022710555344066f75747075740e01000000020000000224134555520972656d61696e646572100200000012000000070fa1c6d5030000"
	if got := hex.EncodeToString(a.Canonical()); got != want {
		t.Fatalf("canonical bytes changed:\n%s", got)
	}
}

func TestTodo_FX_002_Fault(t *testing.T) {
	s := fxPairSource(t, "fault-002", "USD", "EUR")
	p := validFXProfile(t, s.SourceID)
	_, err := ConvertMoney(MoneyConversionRequest{Amount: fxMoney(t, "1.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}})
	if !errors.Is(err, ErrQuoteUnknown) {
		t.Fatalf("missing quote = %v", err)
	}
	zero := fxPairQuote(t, "zero-002", s, "USD", "EUR", "0.000001")
	zero.Rate, _ = values.NewDecimal("0", 6, values.RoundingExactRequired)
	zero.CanonicalDigest = ""
	_, err = ConvertMoney(MoneyConversionRequest{Amount: fxMoney(t, "1.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{zero}})
	if !errors.Is(err, ErrInvalidQuote) {
		t.Fatalf("zero quote = %v", err)
	}
}

func TestTodo_FX_002_Race(t *testing.T) {
	s := fxPairSource(t, "race-002", "USD", "EUR")
	q := fxPairQuote(t, "race-q", s, "USD", "EUR", "0.923456")
	p := validFXProfile(t, s.SourceID)
	req := MoneyConversionRequest{Amount: fxMoney(t, "100.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := ConvertMoney(req)
			if err != nil || r.Converted.String() != "92.35 EUR" {
				t.Errorf("result=%+v err=%v", r, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_FX_002_Security(t *testing.T) {
	s := fxPairSource(t, "security-002", "USD", "EUR")
	q := fxPairQuote(t, "security-q", s, "USD", "EUR", "1.25")
	p := validFXProfile(t, s.SourceID)
	_, err := ConvertMoney(MoneyConversionRequest{Amount: fxMoney(t, "1.00", "USD"), TargetCurrency: "GBP", ViaCurrency: "JPY", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}})
	if !errors.Is(err, ErrQuoteUnknown) {
		t.Fatalf("unapproved triangulation = %v", err)
	}
}

func TestTodo_FX_002_ExactRationalAndPolicyBoundaries(t *testing.T) {
	// The exact quotient is infinitesimally above 62.5. Rounding a reciprocal
	// to scale 18 first erases that distinction and incorrectly returns 62.
	s := fxPairSource(t, "rational-002", "USD", "EUR")
	q := fxPairQuote(t, "rational-q", s, "USD", "EUR", "3.200000")
	rate, err := values.NewDecimal("3.199999999999999999", 18, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	q.Rate = rate
	q, err = NewFXQuoteRevision(q)
	if err != nil {
		t.Fatal(err)
	}
	p := validFXProfile(t, s.SourceID)
	p.RoundingRule.Scale = 0
	p, err = NewConversionProfileRevision(p)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ConvertMoney(MoneyConversionRequest{Amount: fxMoney(t, "200.00", "EUR"), TargetCurrency: "USD", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}})
	if err != nil || r.Converted.String() != "63 USD" {
		t.Fatalf("exact rational inverse = %+v, %v", r, err)
	}

	// The exact product is 0.004999999999999999995. Rounding it to scale 18
	// first produces 0.005 and would then incorrectly half-up to 0.01.
	directSource := fxPairSource(t, "product-boundary", "USD", "EUR")
	directQuote := fxPairQuote(t, "product-boundary-q", directSource, "USD", "EUR", "0.005000")
	directRate, err := values.NewDecimal("0.005000000000000000", 18, values.RoundingHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	directQuote.Rate = directRate
	directQuote, err = NewFXQuoteRevision(directQuote)
	if err != nil {
		t.Fatal(err)
	}
	directAmount, err := values.NewMoney("0.999999999999999999", "USD", 18, values.RoundingHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	directProfile := validFXProfile(t, directSource.SourceID)
	directProfile.RoundingRule = RoundingRule{Scale: 2, Mode: values.RoundingHalfUp}
	directProfile, err = NewConversionProfileRevision(directProfile)
	if err != nil {
		t.Fatal(err)
	}
	directResult, err := ConvertMoney(MoneyConversionRequest{Amount: directAmount, TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: directProfile, Sources: []RateSourceRevision{directSource}, Quotes: []FXQuoteRevision{directQuote}})
	if err != nil || directResult.Converted.String() != "0.00 EUR" {
		t.Fatalf("exact product boundary = %+v, %v", directResult, err)
	}

	one := fxPairSource(t, "policy-one", "USD", "GBP")
	two := fxPairSource(t, "policy-two", "GBP", "JPY")
	q1 := fxPairQuote(t, "policy-q1", one, "USD", "GBP", "1.005000")
	q2 := fxPairQuote(t, "policy-q2", two, "GBP", "JPY", "1.000000")
	tp := validFXProfile(t, one.SourceID, two.SourceID)
	request := MoneyConversionRequest{Amount: fxMoney(t, "1.00", "USD"), TargetCurrency: "JPY", ViaCurrency: "GBP", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: tp, Sources: []RateSourceRevision{one, two}, Quotes: []FXQuoteRevision{q1, q2}}
	if _, err := ConvertMoney(request); !errors.Is(err, ErrQuoteUnknown) {
		t.Fatalf("unapproved intermediary = %v", err)
	}
	tp.TriangulationCurrencies = []string{"GBP"}
	tp, err = NewConversionProfileRevision(tp)
	if err != nil {
		t.Fatal(err)
	}
	request.Profile = tp
	r, err = ConvertMoney(request)
	if err != nil || r.Converted.String() != "1.00 JPY" || r.Legs[0].Output.String() != "1.00 GBP" || r.Remainder.String() != "0.000000000000000000" {
		t.Fatalf("stage rounding = %+v, %v", r, err)
	}
}

func TestTodo_FX_002_ConflictDoesNotFallback(t *testing.T) {
	direct := fxPairSource(t, "conflict-direct", "USD", "EUR")
	q1 := fxPairQuote(t, "conflict-a", direct, "USD", "EUR", "1.100000")
	q2 := fxPairQuote(t, "conflict-b", direct, "USD", "EUR", "1.200000")
	inverse := fxPairSource(t, "conflict-inverse", "EUR", "USD")
	q3 := fxPairQuote(t, "inverse-ok", inverse, "EUR", "USD", "0.900000")
	p := validFXProfile(t, direct.SourceID, inverse.SourceID)
	_, err := ConvertMoney(MoneyConversionRequest{Amount: fxMoney(t, "1.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{direct, inverse}, Quotes: []FXQuoteRevision{q1, q2, q3}})
	if !errors.Is(err, ErrQuoteConflict) {
		t.Fatalf("direct conflict fell back: %v", err)
	}
}

func TestTodo_FX_002_Conformance(t *testing.T) {
	s := fxPairSource(t, "conformance-002", "USD", "EUR")
	q := fxPairQuote(t, "conformance-q", s, "USD", "EUR", "0.923456")
	p := validFXProfile(t, s.SourceID)
	r, err := ConvertMoney(MoneyConversionRequest{Amount: fxMoney(t, "100.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Legs[0].QuoteID != q.QuoteID || r.Legs[0].SourceRevision != q.SourceRevision {
		t.Fatalf("trace lost quote revision: %+v", r.Legs[0])
	}
}

func TestTodo_FX_002_Mutation(t *testing.T) {
	s := fxPairSource(t, "mutation-002", "USD", "EUR")
	q := fxPairQuote(t, "mutation-q", s, "USD", "EUR", "1.25")
	p := validFXProfile(t, s.SourceID)
	r, err := ConvertMoney(MoneyConversionRequest{Amount: fxMoney(t, "1.00", "USD"), TargetCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}})
	if err != nil {
		t.Fatal(err)
	}
	r.Legs[0].Rate = fxRate(t, "9.99")
	if err := r.Validate(); err == nil {
		t.Fatal("mutated result validated")
	}
}

func TestTodo_FX_002_CompatibilityAndValidation(t *testing.T) {
	s := fxPairSource(t, "compat-002", "USD", "EUR")
	p := validFXProfile(t, s.SourceID)
	q := fxPairQuote(t, "compat-q", s, "USD", "EUR", "1.25")
	res, err := ResolveQuote(QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}})
	if err != nil {
		t.Fatal(err)
	}
	amount, err := values.NewDecimal("8.00", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Convert(amount, res, p); err != nil || got.ConvertedAmount.String() != "10.00" {
		t.Fatalf("decimal compatibility = %+v, %v", got, err)
	}
	if got, err := ConvertQuote(amount, q, fxInstant(t, "2026-06-01T12:00:00Z"), p); err != nil || got.ConvertedAmount.String() != "10.00" {
		t.Fatalf("quote compatibility = %+v, %v", got, err)
	}
	store := NewMemoryStore()
	if err := store.SaveRateSource(context.Background(), "tenant", s); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversionProfile(context.Background(), "tenant", p); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadConversionProfile(context.Background(), "tenant", p.ProfileID, p.Revision); err != nil || got.CanonicalDigest != p.CanonicalDigest {
		t.Fatalf("profile load = %+v, %v", got, err)
	}
	if CodeOf(ErrStoreNotFound) != StoreCodeNotFound || (&StoreError{Code: StoreCodeInvalid}).Error() == "" {
		t.Fatal("store error classification failed")
	}
	if _, err := Explain(res); err != nil {
		t.Fatal(err)
	}
	if rebuilt, err := NewFXRateSourceRevision(s); err != nil || len(rebuilt.Canonical()) == 0 {
		t.Fatalf("source compatibility constructor = %+v, %v", rebuilt, err)
	}
	if rebuilt, err := NewQuoteRevision(q); err != nil || len(rebuilt.Canonical()) == 0 {
		t.Fatalf("quote compatibility constructor = %+v, %v", rebuilt, err)
	}
	if rebuilt, err := NewConversionProfile(p); err != nil || len(rebuilt.Canonical()) == 0 {
		t.Fatalf("profile compatibility constructor = %+v, %v", rebuilt, err)
	}
	if resolved, err := Resolve(QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: fxInstant(t, "2026-06-01T12:00:00Z"), KnownAt: fxInstant(t, "2026-06-01T12:00:00Z"), Profile: p, Sources: []RateSourceRevision{s}, Quotes: []FXQuoteRevision{q}}); err != nil || len(resolved.Canonical()) == 0 {
		t.Fatalf("resolution compatibility = %+v, %v", resolved, err)
	}
	if _, err := ConvertMoney(MoneyConversionRequest{Amount: values.Money{}, TargetCurrency: "EUR", Profile: p}); !errors.Is(err, ErrInvalidConversion) {
		t.Fatalf("invalid money = %v", err)
	}
	badProfile := p
	badProfile.CanonicalDigest = ""
	badProfile.TriangulationCurrencies = []string{"GBP", "GBP"}
	if err := badProfile.Validate(); !errors.Is(err, ErrInvalidConversionProfile) {
		t.Fatalf("duplicate triangulation currency = %v", err)
	}
}
