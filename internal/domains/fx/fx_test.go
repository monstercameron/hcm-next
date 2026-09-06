package fx

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
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
