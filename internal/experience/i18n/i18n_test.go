package i18n

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var testTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func validRevision(id, previous string) CatalogRevision {
	return CatalogRevision{ID: id, PreviousRevision: previous, Locale: "en-us", Version: id,
		CreatedAt: testTime, EffectiveFrom: testTime,
		Fallbacks:    map[string][]string{"en-US": {"fr-FR"}},
		Translations: []Translation{{Key: "greeting", Text: "Hello", MeaningID: "greeting.v1", Source: "Hello", Classification: "UI", EffectiveFrom: testTime}}}
}

func TestLocaleAndTranslationCatalogRejectInvalidFallbackAndUnreviewedLegalContent(t *testing.T) {
	if _, err := NewLocaleContext("en-US", []string{"en-US", "fr-FR"}, map[string][]string{"en-US": {"fr-FR"}, "fr-FR": {"en-US"}}); !errors.Is(err, ErrFallbackCycle) {
		t.Fatalf("cycle error = %v", err)
	}
	c := NewCatalog()
	r := validRevision("r1", "")
	r.Translations[0].Classification = "LEGAL_TEXT"
	if err := c.Publish(r); !errors.Is(err, ErrLegalReviewRequired) {
		t.Fatalf("legal error = %v", err)
	}
	if _, ok := c.ActiveRevision("en-US"); ok {
		t.Fatal("invalid publication became active")
	}
	r.Translations[0].Reviewer, r.Translations[0].LegalReviewStatus = "reviewer-1", "APPROVED"
	if err := c.Publish(r); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.ActiveRevision("en-US"); !ok {
		t.Fatal("approved revision is not active")
	}
}

func TestTodo_I18N_001_Property(t *testing.T) {
	supported := []string{"en-US", "fr-FR"}
	fallbacks := map[string][]string{"en-us": {"fr-fr"}}
	c, err := NewLocaleContext("EN_us", supported, fallbacks)
	if err != nil || c.Locale != "en-US" {
		t.Fatalf("normalized locale = %#v, %v", c, err)
	}
	supported[0] = "xx"
	fallbacks["en-us"][0] = "xx"
	path, err := c.FallbackPath("en-US")
	if err != nil || !reflect.DeepEqual(path, []string{"en-US", "fr-FR"}) {
		t.Fatalf("detached context/path = %#v, %v", path, err)
	}
	base := validRevision("r1", "")
	cat := NewCatalog()
	if err := cat.Publish(base); err != nil {
		t.Fatal(err)
	}
	got, _ := cat.Revision("r1")
	got.Fallbacks["en-US"][0] = "de-DE"
	got.Translations[0].Text = "mutated"
	again, _ := cat.Revision("r1")
	if again.Translations[0].Text != "Hello" || again.Fallbacks["en-US"][0] != "fr-FR" {
		t.Fatal("stored revision was mutable through read copy")
	}
	changed := base
	changed.ID = "r2"
	changed.PreviousRevision = "r1"
	changed.Translations[0].Text = "Bonjour"
	changed.Translations[0].MeaningID = "greeting.v2"
	if err := cat.Publish(changed); !errors.Is(err, ErrMeaningChanged) {
		t.Fatalf("meaning change error = %v", err)
	}
}

func TestTodo_I18N_001_Golden(t *testing.T) {
	r := validRevision("r1", "")
	got := DigestRevision(r)
	const want = "554bf2ead404e3257e7da9921f2923e4e0e7341a9189664cccc533caa9ddedac"
	if got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
	if len(got) != 64 || strings.Trim(got, "0123456789abcdef") != "" {
		t.Fatalf("digest is not lowercase sha256 hex: %q", got)
	}
	shuffled := r
	shuffled.Fallbacks = map[string][]string{"fr-FR": {"de-DE"}, "en-US": {"fr-FR"}}
	if DigestRevision(shuffled) == got {
		t.Fatal("material fallback mutation did not change digest")
	}
}

func TestTodo_I18N_001_Race(t *testing.T) {
	c := NewCatalog()
	if err := c.Publish(validRevision("r1", "")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = c.ActiveRevision("en-us")
				_, _ = c.Revision("r1")
			}
		}()
	}
	wg.Wait()
}

func TestTodo_I18N_001_Fault(t *testing.T) {
	var nilCatalog *TranslationCatalog
	if !errors.Is(nilCatalog.Publish(validRevision("r1", "")), ErrInvalidRevision) {
		t.Fatal("nil catalog was accepted")
	}
	c := NewCatalog()
	bad := validRevision("r1", "")
	bad.Translations[0].EffectiveUntil = testTime
	if err := c.Publish(bad); !errors.Is(err, ErrInvalidRevision) {
		t.Fatalf("bad interval = %v", err)
	}
	if _, ok := c.ActiveRevision("en-US"); ok {
		t.Fatal("failed publication changed active state")
	}
	if err := c.Publish(validRevision("r1", "")); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(validRevision("r2", "wrong")); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("conflict = %v", err)
	}
}

func TestTodo_I18N_001_Security(t *testing.T) {
	typ := reflect.TypeOf(LocaleContext{})
	for i := 0; i < typ.NumField(); i++ {
		n := strings.ToLower(typ.Field(i).Name)
		for _, forbidden := range []string{"legal", "jurisdiction", "tax", "residen"} {
			if strings.Contains(n, forbidden) {
				t.Fatalf("locale context exposes authority field %q", typ.Field(i).Name)
			}
		}
	}
	if _, err := NewLocaleContext("de-DE", []string{"en-US"}, nil); !errors.Is(err, ErrUnsupportedLocale) {
		t.Fatalf("unsupported locale = %v", err)
	}
}

func TestTodo_I18N_001_Conformance(t *testing.T) {
	r := validRevision("r1", "")
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if !r.Effective(testTime) {
		t.Fatal("revision not effective at inclusive start")
	}
	if _, ok := r.EffectiveTranslation("greeting", testTime); !ok {
		t.Fatal("translation not effective")
	}
	c := NewCatalog()
	if err := c.Publish(r); err != nil {
		t.Fatal(err)
	}
	got, _ := c.ActiveRevision("en-US")
	if !got.VerifyDigest() || got.CanonicalDigest != got.Digest {
		t.Fatal("published digest is not self-verifying")
	}
}

func TestTodo_I18N_001_Mutation(t *testing.T) {
	r := validRevision("r1", "")
	r.CanonicalDigest = DigestRevision(r)
	if !r.VerifyDigest() {
		t.Fatal("baseline digest does not verify")
	}
	r.Translations[0].Text = "Tampered"
	if r.VerifyDigest() {
		t.Fatal("tampered revision still verified")
	}
}
