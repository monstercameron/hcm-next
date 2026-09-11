package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_020_ClarifiesOrganizationAndDeviceAppearanceScope(t *testing.T) {
	doc, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Theme:     DefaultCustomerTheme(), ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "workspace on this device") {
		t.Fatal("organization-wide color choices must not claim to apply to one device")
	}
	for _, want := range []string{
		`class="surface appearance-scope-guidance"`,
		`id="appearance-scope-title">Organization-wide appearance</h2>`,
		`Saved appearance settings apply to this organization and every signed-in user.`,
		`Use system setting lets each device resolve light or dark from its own system preference`,
		`Light and Dark are organization-wide choices.`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("appearance scope guidance missing %q in %s", want, doc)
		}
	}
}

func TestTodo_UXBLIND_021_ExplainsGovernedLogoAssetPathWithoutInventingUpload(t *testing.T) {
	doc, err := ui.RenderToString(AppearancePage(AppearancePageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Theme:     DefaultCustomerTheme(), ColorModes: ColorModeOptions(), Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(), Navigation: NavigationOptions(), Motions: MotionOptions(), Editable: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="appearance-brand-logo"`, `aria-describedby="appearance-brand-logo-help"`, `id="appearance-brand-logo-help"`,
		`Enter an approved image path supplied by your workspace administrator. This page does not upload or choose files.`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("logo guidance missing %q in %s", want, doc)
		}
	}
	if strings.Contains(strings.ToLower(doc), "upload endpoint") || strings.Contains(strings.ToLower(doc), "asset picker") {
		t.Fatal("logo guidance invented an unavailable upload capability")
	}
}
