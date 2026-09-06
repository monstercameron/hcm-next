package productui

import (
	"strings"
	"testing"
)

func TestDarkModeIsAClosedPersistableAppearanceChoice(t *testing.T) {
	theme := NormalizeCustomerTheme(CustomerTheme{ColorMode: "dark"})
	if theme.ColorMode != "dark" {
		t.Fatalf("dark mode normalized to %q", theme.ColorMode)
	}
	if got := CustomerThemeAttributes(theme)["data-hcm-color-mode"]; got != "dark" {
		t.Fatalf("browser color-mode attribute = %q", got)
	}
	if got := NormalizeCustomerTheme(CustomerTheme{ColorMode: "highlighter"}).ColorMode; got != "system" {
		t.Fatalf("unknown color mode normalized to %q, want system", got)
	}
}

func TestDarkModeUsesSemanticTokensAcrossExplicitAndSystemSchemes(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:root[data-hcm-color-mode="dark"]{color-scheme:dark;`,
		`@media(prefers-color-scheme:dark){:root[data-hcm-color-mode="system"]{color-scheme:dark;`,
		`--canvas:#0b1118`, `--surface:#131c26`, `--ink:#f3f7fb`, `--muted:#aebdcb`,
		`--accent:color-mix(in srgb,var(--hcm-color-brand-primary)`,
		`--hcm-color-success:#69dda2`, `--hcm-color-focus:#d8e9ff`,
		`.surface{background-color:var(--surface);color:var(--ink)}`,
		`.people-filter{background:var(--surface);color:var(--ink)}`,
		`@media(print){:root:is([data-hcm-color-mode="dark"],[data-hcm-color-mode="system"]){color-scheme:light;`,
		`@media(forced-colors:active){:root:is([data-hcm-color-mode="dark"],[data-hcm-color-mode="system"]){color-scheme:light dark;`,
		`.jn-embedded{--jn-canvas:var(--canvas)`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("dark-mode stylesheet missing %q", want)
		}
	}
	if !strings.HasSuffix(css, themeCoverageStyles) {
		t.Fatal("dark mode displaced the final cross-component theme boundary")
	}
}

func TestEveryRenderedPageCarriesTheDarkModeContract(t *testing.T) {
	for _, definition := range PageDefinitions() {
		view := testView(definition.ID)
		view.Appearance = DefaultCustomerTheme()
		view.Appearance.ColorMode = "dark"
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, `data-hcm-color-mode="dark"`) || !strings.Contains(doc, `<meta name="color-scheme" content="light dark">`) {
			t.Errorf("page %q dropped the dark-mode document contract", definition.ID)
		}
	}
}
