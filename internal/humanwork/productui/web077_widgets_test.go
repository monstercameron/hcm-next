package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-077: typed widget bindings validation. Compositions can
// name widget bindings nothing checks against the registered widget
// contracts: an unknown widget type, a future version, an unknown
// authority class, a missing classification or source, a masked
// binding with no display form, or sensitive context bound to an
// external embed would all validate today. The lifecycle's next
// validate step needs a pure binding verdict over the registered
// widget registry with stable reasons.
func TestTodo_WEB_077(t *testing.T) {
	registry := RegisteredWidgets()
	if len(registry.Widgets) == 0 {
		t.Fatal("widget registry is empty")
	}
	tiers := map[string]bool{}
	for _, widget := range registry.Widgets {
		tiers[widget.Tier] = true
	}
	for _, tier := range []string{"governed", "content", "external"} {
		if !tiers[tier] {
			t.Fatalf("widget registry covers no %q tier", tier)
		}
	}
	valid := WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
		Classification: "internal", SourceType: "projection", SourceID: "journeys", Value: "3"}
	for _, binding := range []struct {
		name    string
		binding WidgetBinding
		valid   bool
		reasons []string
	}{
		{"registered binding validates", valid, true, nil},
		{"blank widget refuses", WidgetBinding{}, false, []string{`unknown widget ""`, `unknown authority class ""`, `missing classification`, `missing binding source`}},
		{"unknown widget refuses", WidgetBinding{WidgetType: "teleporter", AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "x"}, false, []string{`unknown widget "teleporter"`}},
		{"future version refuses", WidgetBinding{WidgetType: "metric-display", WidgetVersion: 9, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "x"}, false, []string{`unsupported widget version 9 for "metric-display"`}},
		{"unknown authority refuses", WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "rumor", Classification: "internal", SourceType: "projection", SourceID: "x"}, false, []string{`unknown authority class "rumor"`}},
		{"missing classification refuses", WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", SourceType: "projection", SourceID: "x"}, false, []string{`missing classification`}},
		{"missing source refuses", WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal"}, false, []string{`missing binding source`}},
		{"masked binding needs display", WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "x", Masked: true}, false, []string{`masked binding needs a display value`}},
		{"masked binding with display validates", WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "x", Masked: true, DisplayValue: "•••"}, true, nil},
		{"external embed refuses sensitive context", WidgetBinding{WidgetType: "external-frame", WidgetVersion: 1, AuthorityClass: "external", Classification: "internal", SourceType: "embed", SourceID: "partner", Sensitive: true}, false, []string{`external widget "external-frame" refuses sensitive context`}},
		{"external embed without sensitive context validates", WidgetBinding{WidgetType: "external-frame", WidgetVersion: 1, AuthorityClass: "external", Classification: "public", SourceType: "embed", SourceID: "partner"}, true, nil},
	} {
		verdict := ValidateWidgetBinding(binding.binding, registry)
		if verdict.Compatible != binding.valid || !reflect.DeepEqual(verdict.Reasons, binding.reasons) {
			t.Fatalf("%s = (%t, %q), want (%t, %q)", binding.name, verdict.Compatible, verdict.Reasons, binding.valid, binding.reasons)
		}
	}

	// A draft validates through its widget bindings.
	draft := PageDraft{Page: "studio", Composition: PageComposition{Widgets: []WidgetBinding{valid}}}
	if verdict := ValidateDraftWidgets(draft, registry); !verdict.Compatible {
		t.Fatalf("bound draft fails widget validation: %q", verdict.Reasons)
	}
	broken := draft
	broken.Composition.Widgets = []WidgetBinding{{WidgetType: "teleporter"}}
	if verdict := ValidateDraftWidgets(broken, registry); verdict.Compatible {
		t.Fatal("unbound draft passes widget validation")
	}
}

// Golden: binding validation outcomes over a binding matrix.
func TestTodo_WEB_077_Golden(t *testing.T) {
	registry := RegisteredWidgets()
	bindings := []WidgetBinding{
		{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
		{WidgetType: "teleporter", AuthorityClass: "rumor"},
		{WidgetType: "external-frame", WidgetVersion: 1, AuthorityClass: "external", Classification: "internal", SourceType: "embed", SourceID: "partner", Sensitive: true},
		{WidgetType: "proposal-form", WidgetVersion: 1, AuthorityClass: "manager", Classification: "confidential", SourceType: "workflow", SourceID: "intent-1", Masked: true, DisplayValue: "•••"},
	}
	var builder strings.Builder
	for _, binding := range bindings {
		verdict := ValidateWidgetBinding(binding, registry)
		builder.WriteString(binding.WidgetType)
		builder.WriteString("\x00")
		if verdict.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(verdict.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "7a0f2967524ecbf322e0a04d6279563663ff657bb0c5491ef7e52986d297b75c"
	if got != want {
		t.Fatalf("binding matrix digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget validates a canonical binding
// naming it — deterministically.
func TestTodo_WEB_077_Browser(t *testing.T) {
	registry := RegisteredWidgets()
	for _, widget := range registry.Widgets {
		binding := WidgetBinding{WidgetType: widget.ID, WidgetVersion: widget.Version, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "catalog"}
		first := ValidateWidgetBinding(binding, registry)
		second := ValidateWidgetBinding(binding, registry)
		if !first.Compatible {
			t.Fatalf("widget %q rejects its canonical binding: %q", widget.ID, first.Reasons)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("widget %q validation is nondeterministic", widget.ID)
		}
	}
}

// Conformance: registry determinism, empty-registry refusal, reason
// stability.
func TestTodo_WEB_077_Conformance(t *testing.T) {
	if !reflect.DeepEqual(RegisteredWidgets(), RegisteredWidgets()) {
		t.Fatal("widget registry is nondeterministic")
	}
	empty := WidgetRegistry{}
	verdict := ValidateWidgetBinding(WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "x"}, empty)
	if verdict.Compatible {
		t.Fatal("empty registry validates a binding")
	}
	first := ValidateWidgetBinding(WidgetBinding{WidgetType: "teleporter"}, RegisteredWidgets())
	second := ValidateWidgetBinding(WidgetBinding{WidgetType: "teleporter"}, RegisteredWidgets())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("binding reasons are unstable")
	}
}
