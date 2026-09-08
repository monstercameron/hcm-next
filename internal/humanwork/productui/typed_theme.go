package productui

import (
	"fmt"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// Typed GWC rendering of the customer theme stylesheet (W1). Preset loop
// order and rule order match the original string emitter exactly; the GWC
// serializer emits declarations within one block sorted by property name
// (canonical form), so palette blocks list --hcm-color-brand-hover before
// --hcm-color-brand-primary and glyph rules carry a trailing semicolon.
// Proven semantically identical via /tmp/cssdiff.py (exit 0).
//
// Theme.CSS is intentionally NOT migrated: its single :root block emits
// custom properties in registry order, while the typed path sorts them, so
// no typed rendering reproduces the golden digest pinned in
// theme_test.go TestTodo_WEB_013_Golden (8ac7d3...; first byte differs at
// offset 20: registry `--hcm-color-brand-primary` vs sorted
// `--hcm-color-border`). The pin must pass unchanged.

func declareCustomerThemeStyles() {
	for _, group := range []struct {
		attribute string
		presets   []appearancePreset
	}{
		{"data-hcm-palette", palettePresets},
		{"data-hcm-shape", shapePresets},
		{"data-hcm-density", densityPresets},
		{"data-hcm-motion", motionPresets},
		{"data-hcm-typeface", typefacePresets},
	} {
		for _, preset := range group.presets {
			if len(preset.Overrides) == 0 {
				continue
			}
			theme, err := ResolveTheme(preset.Overrides)
			if err != nil {
				panic(fmt.Sprintf("productui: invalid built-in %s preset %q: %v", group.attribute, preset.Option.ID, err))
			}
			rules := make([]any, 0, len(preset.Overrides))
			for _, token := range registeredThemeTokens {
				value, changed := preset.Overrides[token.Name]
				if !changed {
					continue
				}
				resolved, _ := theme.Value(token.Name)
				if resolved != value {
					panic("productui: preset resolution drift")
				}
				rules = append(rules, gwccss.Custom(token.CSSVariable, resolved))
			}
			declareGlobal(`:root[`+group.attribute+`="`+preset.Option.ID+`"]`, rules...)
		}
	}
	declareGlobal(`:root[data-hcm-glyphs="rounded-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "1.9"),
		gwccss.Raw("stroke-linecap", "round"),
		gwccss.Raw("stroke-linejoin", "round"),
	)
	declareGlobal(`:root[data-hcm-glyphs="precision-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "1.55"),
		gwccss.Raw("stroke-linecap", "square"),
		gwccss.Raw("stroke-linejoin", "miter"),
	)
	declareGlobal(`:root[data-hcm-glyphs="bold-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "2.35"),
		gwccss.Raw("stroke-linecap", "round"),
		gwccss.Raw("stroke-linejoin", "round"),
	)
}
