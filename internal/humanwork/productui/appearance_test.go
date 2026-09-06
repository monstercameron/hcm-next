package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestCustomerThemePresetsCompileIntoTheGovernedStylesheet(t *testing.T) {
	css := Stylesheet()
	for _, expected := range []string{
		`:root[data-hcm-palette="ocean"]{`,
		`:root[data-hcm-shape="rounded"]{`,
		`:root[data-hcm-density="compact"]{`,
		`:root[data-hcm-motion="brisk"]{`,
		`:root[data-hcm-typeface="classic"]{`,
		`:root[data-hcm-glyphs="bold-line"] .nav-icon{`,
		`:root[data-hcm-navigation="brand"] .sidebar`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("governed customer stylesheet missing %q", expected)
		}
	}
	if strings.Contains(css, "data-hcm-focus") || strings.Contains(css, "data-hcm-status") {
		t.Fatal("customer presets gained a selector that could override protected focus or status semantics")
	}
}

func TestCustomerThemeRejectsUnknownStoredChoices(t *testing.T) {
	got := NormalizeCustomerTheme(CustomerTheme{BrandName: "\x00", BrandMark: "<>$", Palette: `red;display:none`, Shape: "unknown", Density: "0", Glyphs: "emoji", Typeface: "remote-font", Navigation: "css", Motion: "infinite"})
	if got != DefaultCustomerTheme() {
		t.Fatalf("unsafe stored theme normalized to %+v, want platform defaults %+v", got, DefaultCustomerTheme())
	}
	attributes := CustomerThemeAttributes(got)
	if len(attributes) != 7 || attributes["data-hcm-palette"] != "evergreen" || attributes["data-hcm-glyphs"] != "rounded-line" || attributes["data-hcm-navigation"] != "light" {
		t.Fatalf("browser theme attributes = %+v", attributes)
	}
}

func TestBrandTextRejectsUnicodeFormattingSpoofs(t *testing.T) {
	got := NormalizeCustomerTheme(CustomerTheme{BrandName: "North\u202eCorp", BrandMark: "N\u202eC"})
	if strings.ContainsRune(got.BrandName, '\u202e') || strings.ContainsRune(got.BrandMark, '\u202e') {
		t.Fatalf("format control survived brand normalization: %+v", got)
	}
	if got.BrandName != "NorthCorp" || got.BrandMark != "NC" {
		t.Fatalf("visible brand text was not preserved: %+v", got)
	}
}

func TestEveryPaletteSupportsWhiteActionAndBrandedNavigationText(t *testing.T) {
	for _, preset := range palettePresets {
		primary := "#006b57"
		if override := preset.Overrides["color.brand.primary"]; override != "" {
			primary = override
		}
		ratio, err := contrastRatio(primary, "#ffffff")
		if err != nil || ratio < 4.5 {
			t.Errorf("palette %q primary %s has white contrast %.2f:1, err=%v", preset.Option.ID, primary, ratio, err)
		}
	}
}

func TestBrandedNavigationAndResponsiveEditorKeepSafetyRules(t *testing.T) {
	css := Stylesheet()
	for _, expected := range []string{
		`:root[data-hcm-navigation="brand"] .sidebar :focus-visible{outline-color:#fff`,
		`@media(max-width:680px){.appearance-brand-fields{grid-template-columns:1fr}`,
		`@media(prefers-reduced-motion:reduce)`,
		`.appearance-status[data-tone="warning"]`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("robust appearance stylesheet missing %q", expected)
		}
	}
}

func TestAppearancePageIsADecomposedAccessibleEditor(t *testing.T) {
	view := testView(PageAppearance)
	view.Appearance = DefaultCustomerTheme()
	doc, err := ui.RenderToString(appearancePage(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`<fieldset`, `>Color palette</legend>`, `name="palette"`, `value="evergreen"`, `checked`,
		`>Surface shape</legend>`, `name="shape"`, `>Glyph set</legend>`, `name="glyphs"`,
		`>Brand signature</legend>`, `name="brand_name"`, `name="brand_mark"`,
		`>Typography character</legend>`, `name="typeface"`, `>Navigation treatment</legend>`, `name="navigation"`,
		`>Motion</legend>`, `role="status"`, `aria-live="polite"`, `>Save appearance</button>`,
	} {
		if !strings.Contains(doc, expected) {
			t.Errorf("appearance editor missing %q", expected)
		}
	}
	if strings.Contains(doc, ` style=`) {
		t.Fatal("appearance editor emitted an inline style instead of the CSP-pinned preset classes")
	}
}

func TestAppearancePageLivesUnderAdminNavigation(t *testing.T) {
	view := testView(PageAppearance)
	_, items := projectNavigation(view)
	admin, ok := projectedNavigationItem(items, PageAdmin)
	if !ok || !admin.Active || !admin.Expanded {
		t.Fatalf("appearance route did not open its Admin parent: %+v", admin)
	}
	child, ok := projectedNavigationItem(admin.Children, PageAppearance)
	if !ok || !child.Active || child.Label != "Brand & appearance" {
		t.Fatalf("appearance navigation child = %+v, present=%t", child, ok)
	}
}

func TestEveryAppearanceOptionHasStableUniqueIdentity(t *testing.T) {
	for name, options := range map[string][]AppearanceOption{
		"palette": PaletteOptions(), "shape": ShapeOptions(), "density": DensityOptions(), "glyph": GlyphOptions(),
		"typeface": TypefaceOptions(), "navigation": NavigationOptions(), "motion": MotionOptions(),
	} {
		seen := map[string]bool{}
		for _, option := range options {
			if option.ID == "" || option.Label == "" || option.Description == "" || seen[option.ID] {
				t.Fatalf("%s option is incomplete or duplicated: %+v", name, option)
			}
			seen[option.ID] = true
		}
	}
}
