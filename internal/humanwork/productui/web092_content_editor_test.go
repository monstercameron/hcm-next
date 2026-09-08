package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-092: the sanitized content editor. Content widgets
// accept sanitized content only, but authors have no governed way
// to set binding values: Value and DisplayValue change only by
// hand-editing with no tier check and no markup check, so raw
// HTML reaches content bindings. The lifecycle needs a pure
// content edit — one binding by index, replacement values —
// refused for non-content tiers, unknown widgets, out-of-range
// targets, raw markup, and display-less masked edits. Markup
// policy deeper than the raw-HTML prohibition stays with the
// content-safety gates at publication.
func TestTodo_WEB_092(t *testing.T) {
	composition := PageComposition{Widgets: []WidgetBinding{
		{WidgetType: "proposal-form", WidgetVersion: 1, AuthorityClass: "manager", Classification: "confidential", SourceType: "workflow", SourceID: "intent-1"},
		{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys", Value: "old"},
	}}
	edited := EditBindingContent(composition, RegisteredWidgets(), ContentEdit{BindingIndex: 1, Value: "3 open requests", DisplayValue: ""})
	if !edited.Compatible {
		t.Fatalf("content edit refuses: %q", edited.Reasons)
	}
	if edited.Composition.Widgets[1].Value != "3 open requests" || edited.Composition.Widgets[1].DisplayValue != "" {
		t.Fatalf("values = %+v", edited.Composition.Widgets[1])
	}
	if edited.Composition.Widgets[0].WidgetType != "proposal-form" || composition.Widgets[1].Value != "old" {
		t.Fatal("editor touched its input or a bystander binding")
	}

	for _, bad := range []struct {
		name   string
		edit   ContentEdit
		reason string
	}{
		{"governed tier", ContentEdit{BindingIndex: 0, Value: "x"}, `widget "proposal-form" is not content-editable`},
		{"no binding", ContentEdit{BindingIndex: 2, Value: "x"}, `unknown binding index 2`},
		{"negative index", ContentEdit{BindingIndex: -1, Value: "x"}, `unknown binding index -1`},
		{"raw markup", ContentEdit{BindingIndex: 1, Value: "click <b>here</b>"}, "content value carries raw markup"},
		{"raw comment", ContentEdit{BindingIndex: 1, Value: "a <!-- note --> b"}, "content value carries raw markup"},
		{"raw display", ContentEdit{BindingIndex: 1, Value: "ok", DisplayValue: "</div>"}, "content display value carries raw markup"},
	} {
		refused := EditBindingContent(composition, RegisteredWidgets(), bad.edit)
		if refused.Compatible {
			t.Fatalf("%s applies", bad.name)
		}
		found := false
		for _, reason := range refused.Reasons {
			if reason == bad.reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s reasons = %q, want %q", bad.name, refused.Reasons, bad.reason)
		}
		if !reflect.DeepEqual(refused.Composition, composition) {
			t.Fatalf("%s mutated the composition", bad.name)
		}
	}

	// Bare angle brackets are not markup; masked bindings still
	// need their display form.
	for _, fine := range []ContentEdit{
		{BindingIndex: 1, Value: "a < b and 3 > 2"},
		{BindingIndex: 1, Value: "count <3"},
		{BindingIndex: 1},
	} {
		if result := EditBindingContent(composition, RegisteredWidgets(), fine); !result.Compatible {
			t.Fatalf("%+v refuses: %q", fine, result.Reasons)
		}
	}
	masked := PageComposition{Widgets: []WidgetBinding{
		{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys", Masked: true},
	}}
	if result := EditBindingContent(masked, RegisteredWidgets(), ContentEdit{BindingIndex: 0, Value: "secret"}); result.Compatible {
		t.Fatal("display-less masked edit applies")
	} else if len(result.Reasons) == 0 || result.Reasons[0] != "masked binding needs a display value" {
		t.Fatalf("masked reasons = %q", result.Reasons)
	}
	if result := EditBindingContent(masked, RegisteredWidgets(), ContentEdit{BindingIndex: 0, Value: "secret", DisplayValue: "•••"}); !result.Compatible {
		t.Fatalf("masked edit with display refuses: %q", result.Reasons)
	}
}

// Golden: content edit outcomes over an edit matrix.
func TestTodo_WEB_092_Golden(t *testing.T) {
	composition := PageComposition{Widgets: []WidgetBinding{
		{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
		{WidgetType: "external-frame", WidgetVersion: 1, AuthorityClass: "external", Classification: "public", SourceType: "embed", SourceID: "partner"},
		{WidgetType: "teleporter", WidgetVersion: 1},
	}}
	edits := []ContentEdit{
		{BindingIndex: 0, Value: "3 open requests"},
		{BindingIndex: 0, Value: "see <b>now</b>"},
		{BindingIndex: 0, Value: "a < b"},
		{BindingIndex: 1, Value: "partner news"},
		{BindingIndex: 2, Value: "mystery"},
		{BindingIndex: 5, Value: "nowhere"},
		{BindingIndex: 0, DisplayValue: "shown"},
		{BindingIndex: 0, Value: "x", DisplayValue: "<i>y</i>"},
	}
	var builder strings.Builder
	for _, edit := range edits {
		result := EditBindingContent(composition, RegisteredWidgets(), edit)
		if result.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(result.Composition.Widgets[0].Value)
		builder.WriteString("\x00")
		builder.WriteString(result.Composition.Widgets[0].DisplayValue)
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(result.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "7dd905ae19be4db5258bc9c2773a8201c02409351ae2138f8629e3134a67c6a4"
	if got != want {
		t.Fatalf("content digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget takes a plain content edit
// exactly when its tier is content — deterministically.
func TestTodo_WEB_092_Browser(t *testing.T) {
	registry := RegisteredWidgets()
	for _, widget := range registry.Widgets {
		composition := PageComposition{Widgets: []WidgetBinding{{WidgetType: widget.ID, WidgetVersion: widget.Version,
			AuthorityClass: "canonical", Classification: widget.ClassificationLimit, SourceType: "projection", SourceID: "catalog"}}}
		first := EditBindingContent(composition, registry, ContentEdit{BindingIndex: 0, Value: "plain words"})
		second := EditBindingContent(composition, registry, ContentEdit{BindingIndex: 0, Value: "plain words"})
		want := widget.Tier == WidgetTierContent
		if first.Compatible != want {
			t.Fatalf("widget %q tier %q edit compatible=%t, want %t (%q)",
				widget.ID, widget.Tier, first.Compatible, want, first.Reasons)
		}
		if want && first.Composition.Widgets[0].Value != "plain words" {
			t.Fatalf("widget %q value not set: %+v", widget.ID, first.Composition)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("widget %q edit is nondeterministic", widget.ID)
		}
	}
}

// Conformance: markup sniff boundaries, input isolation, reason
// stability.
func TestTodo_WEB_092_Conformance(t *testing.T) {
	composition := PageComposition{Widgets: []WidgetBinding{
		{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys", Value: "keep"},
	}}
	for _, fine := range []string{"trailing <", "<3", "&amp;", "http://x/y?a=b", "< script>"} {
		if result := EditBindingContent(composition, RegisteredWidgets(), ContentEdit{BindingIndex: 0, Value: fine}); !result.Compatible {
			t.Fatalf("%q refuses: %q", fine, result.Reasons)
		}
	}
	for _, foul := range []string{"<b>", "</b>", "<br/>", "<!--", "<!DOCTYPE", "<?xml", "<?>", "a<b", "<a href=\"x\">"} {
		if result := EditBindingContent(composition, RegisteredWidgets(), ContentEdit{BindingIndex: 0, Value: foul}); result.Compatible {
			t.Fatalf("%q applies", foul)
		}
	}
	edited := EditBindingContent(composition, RegisteredWidgets(), ContentEdit{BindingIndex: 0, Value: "new"})
	edited.Composition.Widgets[0].Value = "mutated"
	if composition.Widgets[0].Value != "keep" {
		t.Fatal("editor aliases its output")
	}
	bad := EditBindingContent(composition, RegisteredWidgets(), ContentEdit{BindingIndex: 0, Value: "<b>x</b>"})
	again := EditBindingContent(composition, RegisteredWidgets(), ContentEdit{BindingIndex: 0, Value: "<b>x</b>"})
	if !reflect.DeepEqual(bad, again) {
		t.Fatal("content reasons are unstable")
	}
}
