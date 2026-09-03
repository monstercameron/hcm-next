// Package i18n_test is the UX qualification matrix for localized content.
// It deliberately consumes only exported experience-layer contracts: this
// keeps the matrix useful to every presentation adapter and avoids coupling
// qualification to an implementation's private storage.
package i18n_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/experience/i18n"
	"github.com/monstercameron/hcm-next/internal/experience/i18nparity"
)

type catalogFixture struct {
	Locale                string   `json:"locale"`
	Revision              string   `json:"revision"`
	SourceKey             string   `json:"source_key"`
	MeaningID             string   `json:"meaning_id"`
	Text                  string   `json:"text"`
	Direction             string   `json:"direction"`
	Fallbacks             []string `json:"fallbacks"`
	AccessibilityDigest   string   `json:"accessibility_digest"`
	AcknowledgementDigest string   `json:"acknowledgement_digest"`
}

func fixture(t *testing.T) catalogFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("catalog_fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f catalogFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func revision(f catalogFixture, id string, at time.Time) i18n.CatalogRevision {
	return i18n.CatalogRevision{ID: id, Locale: f.Locale, CreatedAt: at,
		Fallbacks: map[string][]string{f.Locale: f.Fallbacks},
		Translations: []i18n.Translation{{Key: f.SourceKey, Text: f.Text, MeaningID: f.MeaningID,
			Source: "uxqual/catalog_fixture.json", Classification: "PUBLIC", EffectiveFrom: at}}}
}

func TestMatrixCatalogRevisionIsImmutableAndMeaningStable(t *testing.T) {
	f := fixture(t)
	now := time.Unix(1700000000, 0).UTC()
	c := i18n.NewCatalog()
	r := revision(f, f.Revision, now)
	if err := c.Publish(r); err != nil {
		t.Fatal(err)
	}
	r.Translations[0].Text = "caller mutation"
	got, ok := c.Revision(f.Revision)
	if !ok || got.Translations[0].Text != f.Text {
		t.Fatalf("published revision leaked input mutation: %#v", got)
	}
	got.Translations[0].Text = "getter mutation"
	again, _ := c.Revision(f.Revision)
	if again.Translations[0].Text != f.Text {
		t.Fatal("revision getter leaked mutable storage")
	}
	changed := revision(f, "ar-sa-r2", now.Add(time.Hour))
	changed.PreviousRevision = f.Revision
	changed.Translations[0].MeaningID = "different-meaning"
	if !errors.Is(c.Publish(changed), i18n.ErrMeaningChanged) {
		t.Fatal("meaning change was accepted")
	}
}

func TestMatrixDeterministicFormattingAndBidiDirection(t *testing.T) {
	f := fixture(t)
	c := i18nparity.NewRegistry()
	d := i18nparity.Derivative{ID: "notice-ar", Kind: string(i18nparity.MessageDerivative), SourceKey: f.SourceKey,
		Locale: f.Locale, CatalogRevision: f.Revision, MeaningID: f.MeaningID, Text: f.Text, Direction: f.Direction,
		AccessibilityDigest: f.AccessibilityDigest, AcknowledgementDigest: f.AcknowledgementDigest}
	if err := c.Register(d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Text, "\u2068") || !strings.Contains(d.Text, "\u2069") {
		t.Fatal("RTL fixture lacks isolate boundaries")
	}
	if d.Direction != "rtl" {
		t.Fatalf("direction = %q, want rtl", d.Direction)
	}
	a, err := c.Render(d, i18nparity.Desktop)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		b, err := c.Render(d, i18nparity.Desktop)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("non-deterministic render at iteration %d: %#v != %#v", i, a, b)
		}
	}
	if got := i18nparity.AcknowledgementDigest(a); got != i18nparity.AcknowledgementDigest(a) {
		t.Fatal("acknowledgement digest is not deterministic")
	}
}

func TestMatrixInvalidationIsDependencyExact(t *testing.T) {
	f := fixture(t)
	c := i18nparity.NewRegistry()
	base := i18nparity.Derivative{Kind: "message", Locale: f.Locale, CatalogRevision: f.Revision, Text: f.Text, Direction: "rtl", AccessibilityDigest: "a", AcknowledgementDigest: "b"}
	for _, d := range []i18nparity.Derivative{{ID: "same-key", SourceKey: f.SourceKey}, {ID: "other-key", SourceKey: "profile.updated"}, {ID: "other-locale", SourceKey: f.SourceKey, Locale: "en-US"}} {
		d.Kind, d.Text, d.Direction, d.AccessibilityDigest, d.AcknowledgementDigest = base.Kind, base.Text, base.Direction, base.AccessibilityDigest, base.AcknowledgementDigest
		if err := c.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	got := c.Invalidate(i18nparity.TranslationChange{Locale: f.Locale, SourceKey: f.SourceKey, PreviousRevision: f.Revision, NewRevision: "ar-sa-r2"})
	if !reflect.DeepEqual(got, []string{"same-key"}) {
		t.Fatalf("invalidated = %#v", got)
	}
	if c.Active("other-key") == false || c.Active("other-locale") == false {
		t.Fatal("unrelated derivative was invalidated")
	}
	if _, err := c.RenderID("same-key", i18nparity.Desktop); !errors.Is(err, i18nparity.ErrInvalidated) {
		t.Fatalf("stale render error = %v", err)
	}
}

func TestMatrixEveryChannelCarriesIdenticalSemanticRender(t *testing.T) {
	f := fixture(t)
	c := i18nparity.NewRegistry()
	d := i18nparity.Derivative{ID: "parity", Kind: "message", SourceKey: f.SourceKey, Locale: f.Locale, CatalogRevision: f.Revision, MeaningID: f.MeaningID, Text: f.Text, Direction: "rtl", AccessibilityDigest: "a", AcknowledgementDigest: "b"}
	if err := c.Register(d); err != nil {
		t.Fatal(err)
	}
	all, err := c.RenderAcrossChannels(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(i18nparity.Channels) {
		t.Fatalf("channels = %d, want %d", len(all), len(i18nparity.Channels))
	}
	for _, ch := range i18nparity.Channels {
		if !reflect.DeepEqual(all[ch], all[i18nparity.Desktop]) {
			t.Fatalf("channel %s changed semantic render", ch)
		}
	}
	if _, err := c.Render(d, i18nparity.Channel("unknown")); !errors.Is(err, i18nparity.ErrUnsupportedChannel) {
		t.Fatalf("unknown channel error = %v", err)
	}
}
