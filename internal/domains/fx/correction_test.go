package fx

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func correctionQuote(t *testing.T, id string, rev uint64, parent, digest string, rate string) FXQuoteRevision {
	t.Helper()
	d, err := values.NewDecimal(rate, 6, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	knownAt := "2026-06-01T11:00:00Z"
	if rev > 1 {
		knownAt = "2026-06-01T12:00:00Z"
	}
	q, err := NewFXQuoteRevision(FXQuoteRevision{QuoteID: id, Revision: rev, ParentQuoteID: parent, ParentDigest: digest, SourceID: "src", SourceRevision: 1, BaseCurrency: "USD", QuoteCurrency: "EUR", Rate: d, AsOf: values.NewInstant(mustTime("2026-06-01T10:00:00Z")), KnownAt: values.NewInstant(mustTime(knownAt)), MarketConvention: ConventionSpot, Confidence: ConfidenceHigh})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func mustTime(s string) (out time.Time) { out, _ = time.Parse(time.RFC3339, s); return }

func TestFXCorrectionAppendsSuccessorAndRecomputesOnlyAffectedResults(t *testing.T) {
	old := correctionQuote(t, "q1", 1, "", "", "0.923456")
	newQ, err := CorrectQuote(old, correctionQuote(t, "q2", 2, old.QuoteID, old.CanonicalDigest, "0.923400"))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := BuildImpactGraph(old, newQ, []DerivedDependency{
		{ResultID: "r-approved", QuoteID: old.QuoteID, QuoteDigest: old.CanonicalDigest, BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: old.AsOf, KnownAt: old.KnownAt, Approved: true},
		{ResultID: "r-other-quote", QuoteID: "q-other", QuoteDigest: old.CanonicalDigest, BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: old.AsOf, KnownAt: old.KnownAt},
	})
	if err != nil || len(graph.Entries) != 1 || !graph.Entries[0].ReplanRequired || graph.Entries[0].ResultID != "r-approved" {
		t.Fatalf("graph=%+v err=%v", graph, err)
	}
	if old.QuoteID == newQ.QuoteID || old.CanonicalDigest == newQ.CanonicalDigest {
		t.Fatal("correction rewrote or reused original identity")
	}
}

func TestTodo_FX_003_Golden(t *testing.T) {
	old := correctionQuote(t, "q1", 1, "", "", "0.923456")
	newQ, err := CorrectQuote(old, correctionQuote(t, "q2", 2, old.QuoteID, old.CanonicalDigest, "0.923400"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildImpactGraph(old, newQ, []DerivedDependency{{ResultID: "r", QuoteID: old.QuoteID, QuoteDigest: old.CanonicalDigest, BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: old.AsOf, KnownAt: old.KnownAt}})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:2a2ebd495ec2600aed2874846f3a5eb1cacf1fb5eeb48ae6528310c4cae6c0a0"
	if g.CanonicalDigest != want {
		t.Fatalf("digest=%s want %s", g.CanonicalDigest, want)
	}
}

func TestTodo_FX_003_Property(t *testing.T) {
	old := correctionQuote(t, "q1", 1, "", "", "0.923456")
	if _, err := CorrectQuote(old, old); !errors.Is(err, ErrQuoteSuccessorRequired) {
		t.Fatalf("in-place correction=%v", err)
	}
	equalKnown := correctionQuote(t, "q2-equal-known", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
	equalKnown.KnownAt = old.KnownAt
	equalKnown.CanonicalDigest = ""
	if _, err := CorrectQuote(old, equalKnown); !errors.Is(err, ErrInvalidQuote) {
		t.Fatalf("non-monotonic correction known-at = %v", err)
	}
	later := correctionQuote(t, "q2-later", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
	if corrected, err := CorrectQuote(old, later); err != nil || corrected.AsOf.Compare(old.AsOf) != 0 || !old.KnownAt.Before(corrected.KnownAt) {
		t.Fatalf("bitemporal correction = %+v, err=%v", corrected, err)
	}
	for _, d := range []DerivedDependency{{QuoteID: old.QuoteID, QuoteDigest: "wrong", BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: old.AsOf, KnownAt: old.KnownAt}, {QuoteID: old.QuoteID, QuoteDigest: old.CanonicalDigest, BaseCurrency: "USD", QuoteCurrency: "EUR"}} {
		q2 := correctionQuote(t, "q2", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
		g, err := BuildImpactGraph(old, q2, []DerivedDependency{d})
		if d.QuoteDigest == old.CanonicalDigest {
			if !errors.Is(err, ErrInvalidImpactGraph) {
				t.Fatalf("ambiguous dependency err=%v", err)
			}
			continue
		}
		if err != nil || len(g.Entries) != 0 {
			t.Fatalf("unscoped dependency graph=%+v err=%v", g, err)
		}
	}
}

func TestTodo_FX_003_Race(t *testing.T) {
	old := correctionQuote(t, "q1", 1, "", "", "0.923456")
	q2 := correctionQuote(t, "q2", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
	dep := []DerivedDependency{{ResultID: "r", QuoteID: old.QuoteID, QuoteDigest: old.CanonicalDigest, BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: old.AsOf, KnownAt: old.KnownAt}}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, err := BuildImpactGraph(old, q2, dep)
			if err != nil || len(g.Entries) != 1 {
				t.Errorf("graph=%+v err=%v", g, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_FX_003_Conformance(t *testing.T) {
	old := correctionQuote(t, "q-history", 1, "", "", "0.923456")
	next := correctionQuote(t, "q-current", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
	pair, err := NewCurrencyPair("USD", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewRateSourceRevision(RateSourceRevision{SourceID: "src", Revision: 1, ProviderRef: "provider", Pairs: []CurrencyPair{pair}, QuoteCadence: CadenceHourly, AuthorityClass: AuthorityPrimary, Effective: mustOpenInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := NewConversionProfileRevision(ConversionProfileRevision{ProfileID: "history", Revision: 1, RoundingRule: RoundingRule{Scale: 2, Mode: values.RoundingHalfEven}, Tolerance: 24 * time.Hour, FallbackSourceOrder: []string{source.SourceID}, Effective: mustOpenInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	request := QuoteResolutionRequest{BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: values.NewInstant(mustTime("2026-06-01T10:30:00Z")), KnownAt: values.NewInstant(mustTime("2026-06-01T11:30:00Z")), Profile: profile, Sources: []RateSourceRevision{source}, Quotes: []FXQuoteRevision{old, next}}
	historical, err := ResolveQuote(request)
	if err != nil || historical.Quote.QuoteID != old.QuoteID {
		t.Fatalf("historical cutoff = %+v, err=%v", historical, err)
	}
	request.KnownAt = values.NewInstant(mustTime("2026-06-01T12:30:00Z"))
	current, err := ResolveQuote(request)
	if err != nil || current.Quote.QuoteID != next.QuoteID {
		t.Fatalf("current cutoff = %+v, err=%v", current, err)
	}
}

func TestTodo_FX_003_Fault(t *testing.T) {
	old := correctionQuote(t, "q-fault", 1, "", "", "0.923456")
	forged := correctionQuote(t, "q-forged", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
	forged.ParentDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	forged.CanonicalDigest = ""
	if _, err := CorrectQuote(old, forged); !errors.Is(err, ErrQuoteSuccessorRequired) {
		t.Fatalf("forged predecessor digest = %v", err)
	}
}

func TestTodo_FX_003_Security(t *testing.T) {
	old := correctionQuote(t, "q-usd-eur", 1, "", "", "0.923456")
	next := correctionQuote(t, "q-usd-eur-next", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
	graph, err := BuildImpactGraph(old, next, []DerivedDependency{
		{ResultID: "usd-eur", QuoteID: old.QuoteID, QuoteDigest: old.CanonicalDigest, BaseCurrency: "USD", QuoteCurrency: "EUR", AsOf: old.AsOf, KnownAt: old.KnownAt},
		{ResultID: "gbp-jpy", QuoteID: "triangulation-leg", QuoteDigest: old.CanonicalDigest, BaseCurrency: "GBP", QuoteCurrency: "JPY", AsOf: old.AsOf, KnownAt: old.KnownAt},
	})
	if err != nil || len(graph.Entries) != 1 || graph.Entries[0].ResultID != "usd-eur" {
		t.Fatalf("independent currency impact = %+v, err=%v", graph, err)
	}
}

func TestTodo_FX_003_Mutation(t *testing.T) {
	s := NewMemoryStore()
	pair, err := NewCurrencyPair("USD", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewRateSourceRevision(RateSourceRevision{SourceID: "src", Revision: 1, ProviderRef: "provider", Pairs: []CurrencyPair{pair}, QuoteCadence: CadenceHourly, AuthorityClass: AuthorityPrimary, Effective: mustOpenInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRateSource(context.Background(), "tenant-a", source); err != nil {
		t.Fatal(err)
	}
	old := correctionQuote(t, "q1", 1, "", "", "0.923456")
	if err := s.SaveQuote(context.Background(), "tenant-a", old); err != nil {
		t.Fatal(err)
	}
	next := correctionQuote(t, "q2", 2, old.QuoteID, old.CanonicalDigest, "0.923400")
	if err := s.SaveQuoteSuccessor(context.Background(), "tenant-a", next); err != nil {
		t.Fatalf("successor append=%v (source intentionally absent should be reference refusal)", err)
	}
	got, err := s.LoadQuote(context.Background(), "tenant-a", old.QuoteID)
	if err != nil || got.CanonicalDigest != old.CanonicalDigest {
		t.Fatalf("predecessor=%+v err=%v", got, err)
	}
}

func mustOpenInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	i, err := values.NewOpenInstantInterval(values.NewInstant(mustTime("2026-01-01T00:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	return i
}
