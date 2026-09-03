package i18nparity

import (
	"sync"
	"testing"
)

func derivative(id, key, rev, text string) Derivative {
	return Derivative{ID: id, Kind: string(Document), SourceKey: key, Locale: "en-US", CatalogRevision: rev, MeaningID: "meaning:" + key, Reviewer: "reviewer-1", Text: text, Direction: "ltr", AccessibilityDigest: "wcag:1", AcknowledgementDigest: "ack:1", Legal: true}
}

func TestLegalTranslationChangeInvalidatesRenderedDerivativesAcrossChannels(t *testing.T) {
	r := NewRegistry()
	for _, kind := range []DerivativeKind{Document, MessageDerivative, Form, Knowledge, GWC} {
		d := derivative(string(kind), "pay.notice", "v1", "Old notice")
		d.Kind = string(kind)
		if err := r.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	if got := r.InvalidateTranslationChange(TranslationChange{Locale: "en-US", SourceKey: "pay.notice", PreviousRevision: "v1", NewRevision: "v2", Legal: true}); len(got) != 5 {
		t.Fatalf("invalidated %d derivatives", len(got))
	}
	for _, ch := range Channels {
		if _, err := r.Render(derivative("fresh-"+string(ch), "pay.notice", "v2", "New notice"), ch); err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
	}
}

func TestTodo_I18N_003_Conformance(t *testing.T) {
	if len(Channels) != 5 || Channels[0] != Desktop || Channels[1] != Mobile || Channels[2] != Kiosk || Channels[3] != Message || Channels[4] != Assistive {
		t.Fatalf("channel registry changed: %#v", Channels)
	}
	r := derivative("d", "k", "v1", "same")
	got, err := NewRegistry().RenderAcrossChannels(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range Channels {
		if !EqualRender(got[Desktop], got[ch]) {
			t.Fatalf("semantic mismatch for %s", ch)
		}
	}
}

func TestTodo_I18N_003_Golden(t *testing.T) {
	d := derivative("d", "k", "v1", "same")
	if AcknowledgementDigest(Render{SourceKey: d.SourceKey, Locale: d.Locale, CatalogRevision: d.CatalogRevision, MeaningID: d.MeaningID, Text: d.Text, Direction: d.Direction, AccessibilityDigest: d.AccessibilityDigest, AcknowledgementDigest: d.AcknowledgementDigest}) == "" {
		t.Fatal("empty digest")
	}
}

func TestTodo_I18N_003_Property(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(derivative("a", "one", "v1", "x"))
	_ = r.Register(derivative("b", "two", "v1", "x"))
	r.Invalidate(TranslationChange{Locale: "en-US", SourceKey: "one", PreviousRevision: "v1", NewRevision: "v2"})
	if r.Active("a") || !r.Active("b") {
		t.Fatal("invalidation was not dependency-exact")
	}
}

func TestTodo_I18N_003_Race(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(derivative("a", "k", "v1", "x"))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Invalidate(TranslationChange{Locale: "en-US", SourceKey: "k", PreviousRevision: "v1", NewRevision: "v2"})
			r.Derivative("a")
		}()
	}
	wg.Wait()
}

func TestTodo_I18N_003_Fault(t *testing.T) {
	if _, err := NewRegistry().Render(derivative("d", "k", "v1", "x"), Channel("unknown")); err != ErrUnsupportedChannel {
		t.Fatalf("err=%v", err)
	}
}
func TestTodo_I18N_003_Security(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(derivative("d", "legal", "v1", "old"))
	r.Invalidate(TranslationChange{Locale: "en-US", SourceKey: "legal", PreviousRevision: "v1", NewRevision: "v2", Legal: true})
	if _, err := r.RenderID("d", Desktop); err != ErrInvalidated {
		t.Fatalf("stale derivative rendered: %v", err)
	}
}
func TestTodo_I18N_003_Mutation(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(derivative("d", "k", "v1", "x"))
	if got := r.Invalidate(TranslationChange{Locale: "de-DE", SourceKey: "k", PreviousRevision: "v1", NewRevision: "v2"}); len(got) != 0 {
		t.Fatal("wrong locale invalidated derivative")
	}
}
