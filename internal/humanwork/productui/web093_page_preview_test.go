package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-093: multidimensional page preview. Compositions
// reach publication with no governed preview: nothing plans
// which locale, color-mode, and viewport cases an author reviews
// with which fixture, so coverage is author memory. The
// lifecycle needs a pure preview plan — page, fixture, and
// registered dimension axes with cases — validated against the
// product contracts and expanded into a stable case matrix.
// Axes register only what presentation renders; deeper render
// truth stays with the renderer.
func TestTodo_WEB_093(t *testing.T) {
	plan := PreviewPlan{Page: "studio", Fixture: "journeys-live",
		Dimensions: []PreviewDimension{
			{Axis: PreviewAxisLocale, Cases: []string{"en-US", "ar"}},
			{Axis: PreviewAxisColorMode, Cases: []string{"light", "dark"}},
			{Axis: PreviewAxisViewportWidth, Cases: []string{"320", "1280"}},
		}}
	if verdict := ValidatePreviewPlan(plan); !verdict.Compatible {
		t.Fatalf("preview plan refuses: %q", verdict.Reasons)
	}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatalf("expansion errors: %v", err)
	}
	if len(cases) != 8 {
		t.Fatalf("expansion lists %d cases, want 2x2x2", len(cases))
	}
	if cases[0].ID != "locale=en-US|color-mode=light|viewport-width=320" {
		t.Fatalf("first case ID = %q", cases[0].ID)
	}
	if last := cases[len(cases)-1]; last.ID != "locale=ar|color-mode=dark|viewport-width=1280" {
		t.Fatalf("last case ID = %q", last.ID)
	}
	for _, cell := range cases {
		if len(cell.Values) != 3 || cell.Values[0].Axis != PreviewAxisLocale {
			t.Fatalf("case values out of dimension order: %+v", cell)
		}
	}

	for _, bad := range []struct {
		name   string
		plan   PreviewPlan
		reason string
	}{
		{"missing page", PreviewPlan{Fixture: "f"}, "missing preview page"},
		{"missing fixture", PreviewPlan{Page: "studio"}, "missing preview fixture"},
		{"unknown axis", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "population", Cases: []string{"x"}}}}, `unknown preview axis "population"`},
		{"duplicate axis", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "locale", Cases: []string{"en-US"}}, {Axis: "locale", Cases: []string{"ar"}}}}, `duplicate preview axis "locale"`},
		{"blank case", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "locale", Cases: []string{""}}}}, `blank preview case for axis "locale"`},
		{"duplicate case", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "locale", Cases: []string{"ar", "ar"}}}}, `duplicate preview case "ar" for axis "locale"`},
		{"bad locale", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "locale", Cases: []string{"fr-FR"}}}}, `unsupported preview locale "fr-FR"`},
		{"system color", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "color-mode", Cases: []string{"system"}}}}, `unsupported preview color mode "system"`},
		{"zero width", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "viewport-width", Cases: []string{"0"}}}}, `unsupported preview viewport width "0"`},
		{"word width", PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "viewport-width", Cases: []string{"wide"}}}}, `unsupported preview viewport width "wide"`},
	} {
		verdict := ValidatePreviewPlan(bad.plan)
		if verdict.Compatible {
			t.Fatalf("%s validates", bad.name)
		}
		found := false
		for _, reason := range verdict.Reasons {
			if reason == bad.reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s reasons = %q, want %q", bad.name, verdict.Reasons, bad.reason)
		}
		if _, err := ExpandPreviewPlan(bad.plan); err == nil {
			t.Fatalf("%s expands", bad.name)
		}
	}

	// Empty dimensions preview the default render as one caseless cell.
	single, err := ExpandPreviewPlan(PreviewPlan{Page: "studio", Fixture: "f"})
	if err != nil {
		t.Fatalf("default preview errors: %v", err)
	}
	if len(single) != 1 || single[0].ID != "default" || len(single[0].Values) != 0 {
		t.Fatalf("default preview = %+v", single)
	}
}

// Golden: plan verdicts and case IDs over a plan matrix.
func TestTodo_WEB_093_Golden(t *testing.T) {
	plans := []PreviewPlan{
		{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
			{Axis: PreviewAxisLocale, Cases: []string{"en-US", "ar"}},
			{Axis: PreviewAxisViewportWidth, Cases: []string{"320"}},
		}},
		{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
			{Axis: PreviewAxisColorMode, Cases: []string{"dark", "light"}},
		}},
		{Page: "studio", Dimensions: []PreviewDimension{{Axis: PreviewAxisLocale, Cases: []string{"de-DE"}}}},
		{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: "population", Cases: []string{"x"}}}},
		{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{{Axis: PreviewAxisLocale, Cases: []string{"en-US", "xx"}}}},
		{Page: "studio", Fixture: "f"},
	}
	var builder strings.Builder
	for _, plan := range plans {
		verdict := ValidatePreviewPlan(plan)
		if verdict.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(verdict.Reasons, ";"))
		builder.WriteString("\x00")
		cases, err := ExpandPreviewPlan(plan)
		if err != nil {
			builder.WriteString("no-expansion")
			builder.WriteString("\n")
			continue
		}
		for _, cell := range cases {
			builder.WriteString(cell.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "fda9805339d6c02eda6d146be676d91200d545e3987fa5aa27721a076db0e93c"
	if got != want {
		t.Fatalf("preview digest = %s, want %s", got, want)
	}
}

// Browser: the full contract matrix — every supported locale,
// both color modes, narrow and wide viewports — expands
// deterministically with stable IDs.
func TestTodo_WEB_093_Browser(t *testing.T) {
	plan := PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: SupportedProductLocales()},
		{Axis: PreviewAxisColorMode, Cases: []string{"light", "dark"}},
		{Axis: PreviewAxisViewportWidth, Cases: []string{"320", "1280"}},
	}}
	first, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 12 {
		t.Fatalf("contract matrix lists %d cases, want 12", len(first))
	}
	seen := map[string]bool{}
	for _, cell := range first {
		if cell.ID == "" || seen[cell.ID] {
			t.Fatalf("case ID missing or duplicated: %+v", first)
		}
		seen[cell.ID] = true
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("preview expansion is nondeterministic")
	}
}

// Conformance: cartesian order, axis validation independence,
// expansion errors join verdict reasons.
func TestTodo_WEB_093_Conformance(t *testing.T) {
	plan := PreviewPlan{Page: "studio", Fixture: "f", Dimensions: []PreviewDimension{
		{Axis: PreviewAxisLocale, Cases: []string{"ar"}},
		{Axis: PreviewAxisColorMode, Cases: []string{"dark"}},
	}}
	cases, err := ExpandPreviewPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 || cases[0].ID != "locale=ar|color-mode=dark" {
		t.Fatalf("single-cell matrix = %+v", cases)
	}
	bad := PreviewPlan{Dimensions: []PreviewDimension{{Axis: "nope", Cases: []string{""}}}}
	verdict := ValidatePreviewPlan(bad)
	if len(verdict.Reasons) == 0 {
		t.Fatal("compound refusal carries no reasons")
	}
	_, err = ExpandPreviewPlan(bad)
	if err == nil || !strings.Contains(err.Error(), verdict.Reasons[0]) {
		t.Fatalf("expansion error %v omits verdict reasons %q", err, verdict.Reasons)
	}
	if verdict.Compatible {
		t.Fatal("compound plan validates")
	}
}
