package i18n

import (
	"errors"
	"testing"
	"time"
)

func TestLocaleAndTranslationCatalogRejectInvalidFallbackAndUnreviewedLegalContent(t *testing.T) {
	if _, err := NewLocaleContext("en-US", []string{"en-US", "fr-FR"}, map[string][]string{"en-US": {"fr-FR"}, "fr-FR": {"en-US"}}); !errors.Is(err, ErrFallbackCycle) {
		t.Fatalf("cycle error = %v", err)
	}
	if _, err := NewLocaleContext("de-DE", []string{"en-US"}, nil); !errors.Is(err, ErrUnsupportedLocale) {
		t.Fatalf("unsupported error = %v", err)
	}
	c := NewTranslationCatalog()
	now := time.Unix(100, 0).UTC()
	bad := CatalogRevision{ID: "r1", Locale: "en-US", CreatedAt: now, Translations: []Translation{{Key: "leave.notice", Text: "text", Source: "legal/1", Classification: "LEGAL", EffectiveFrom: now, Legal: true}}}
	if err := c.PublishRevision(bad); !errors.Is(err, ErrLegalReviewRequired) {
		t.Fatalf("legal error = %v", err)
	}
	bad = CatalogRevision{ID: "cycle", Locale: "en-US", CreatedAt: now, Fallbacks: map[string][]string{"en-US": {"fr-FR"}, "fr-FR": {"en-US"}}}
	if err := c.PublishRevision(bad); !errors.Is(err, ErrFallbackCycle) {
		t.Fatalf("revision cycle error = %v", err)
	}
}

func TestCatalogRevisionsAreImmutableAndKeyMeaningCannotChange(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	c := NewTranslationCatalog()
	r1 := CatalogRevision{ID: "r1", Locale: "en-US", CreatedAt: now, Translations: []Translation{{Key: "greeting", Text: "Hello", MeaningID: "greeting.v1", Source: "copy/greeting", Classification: "PUBLIC", EffectiveFrom: now}}}
	if err := c.PublishRevision(r1); err != nil {
		t.Fatal(err)
	}
	r1.Translations[0].Text = "mutated"
	got, ok := c.Revision("r1")
	if !ok || got.Translations[0].Text != "Hello" {
		t.Fatalf("stored revision mutated: %#v", got)
	}
	got.Translations[0].Text = "caller mutation"
	gotAgain, _ := c.Revision("r1")
	if gotAgain.Translations[0].Text != "Hello" {
		t.Fatal("revision getter leaked mutable storage")
	}
	r2 := CatalogRevision{ID: "r2", Locale: "en-US", PreviousRevision: "r1", CreatedAt: now.Add(time.Hour), Translations: []Translation{{Key: "greeting", Text: "Hi", MeaningID: "other.meaning", Source: "copy/greeting", Classification: "PUBLIC", EffectiveFrom: now}}}
	if err := c.PublishRevision(r2); !errors.Is(err, ErrMeaningChanged) {
		t.Fatalf("meaning error = %v", err)
	}
	if _, ok := c.ActiveRevision("en-US"); !ok {
		t.Fatal("active revision disappeared after rejected publication")
	}
}
