package productui

import (
	"path"
	"strings"
	"unicode"
)

// CustomerTheme is the bounded, customer-selectable BrandPack presentation
// layer used by the product shell. Every value is an identifier from a closed
// preset registry; no customer-authored CSS reaches the document.
type CustomerTheme struct {
	BrandName    string `json:"brand_name"`
	BrandMark    string `json:"brand_mark"`
	BrandLogoURL string `json:"brand_logo_url,omitempty"`
	ColorMode    string `json:"color_mode"`
	Palette      string `json:"palette"`
	Shape        string `json:"shape"`
	Density      string `json:"density"`
	Glyphs       string `json:"glyphs"`
	Typeface     string `json:"typeface"`
	Navigation   string `json:"navigation"`
	Motion       string `json:"motion"`
}

// AppearanceOption is a reusable editor choice. Swatches are semantic color
// values rather than CSS snippets so the component cannot become an injection
// boundary.
type AppearanceOption struct {
	ID          string
	Label       string
	Description string
	Swatches    []string
}

type appearancePreset struct {
	Option    AppearanceOption
	Overrides map[string]string
}

var palettePresets = []appearancePreset{
	{Option: AppearanceOption{ID: "evergreen", Label: "Evergreen", Description: "Grounded and people-centered", Swatches: []string{"#006b57", "#eaf3ef", "#102238"}}},
	{Option: AppearanceOption{ID: "ocean", Label: "Ocean", Description: "Clear and operational", Swatches: []string{"#1555a3", "#eaf1fb", "#13243a"}}, Overrides: map[string]string{
		"color.brand.primary": "#1555a3", "color.brand.hover": "#0d407f", "color.brand.soft": "#eaf1fb", "color.text.primary": "#13243a", "color.text.muted": "#52647a",
	}},
	{Option: AppearanceOption{ID: "plum", Label: "Plum", Description: "Warm and distinctive", Swatches: []string{"#7a285f", "#f7eaf2", "#2e1c29"}}, Overrides: map[string]string{
		"color.brand.primary": "#7a285f", "color.brand.hover": "#5f1d49", "color.brand.soft": "#f7eaf2", "color.text.primary": "#2e1c29", "color.text.muted": "#66515f",
	}},
	{Option: AppearanceOption{ID: "graphite", Label: "Graphite", Description: "Neutral and understated", Swatches: []string{"#344054", "#edf0f3", "#182230"}}, Overrides: map[string]string{
		"color.brand.primary": "#344054", "color.brand.hover": "#1d2939", "color.brand.soft": "#edf0f3", "color.text.primary": "#182230", "color.text.muted": "#536273",
	}},
}

var shapePresets = []appearancePreset{
	{Option: AppearanceOption{ID: "balanced", Label: "Balanced", Description: "Crisp controls with softly rounded surfaces"}},
	{Option: AppearanceOption{ID: "precise", Label: "Precise", Description: "Tighter corners for a technical character"}, Overrides: map[string]string{"radius.control": "4px", "radius.surface": "6px"}},
	{Option: AppearanceOption{ID: "rounded", Label: "Rounded", Description: "Friendlier controls and generous surfaces"}, Overrides: map[string]string{"radius.control": "12px", "radius.surface": "18px"}},
}

var densityPresets = []appearancePreset{
	{Option: AppearanceOption{ID: "compact", Label: "Compact", Description: "More information in each viewport"}, Overrides: map[string]string{"spacing.density": ".875"}},
	{Option: AppearanceOption{ID: "comfortable", Label: "Comfortable", Description: "Balanced default spacing"}},
	{Option: AppearanceOption{ID: "spacious", Label: "Spacious", Description: "More breathing room around content"}, Overrides: map[string]string{"spacing.density": "1.125"}},
}

var glyphPresets = []appearancePreset{
	{Option: AppearanceOption{ID: "rounded-line", Label: "Rounded line", Description: "Soft terminals and approachable symbols"}},
	{Option: AppearanceOption{ID: "precision-line", Label: "Precision line", Description: "Crisp terminals and restrained weight"}},
	{Option: AppearanceOption{ID: "bold-line", Label: "Bold line", Description: "Higher-emphasis symbols for busy screens"}},
}

var motionPresets = []appearancePreset{
	{Option: AppearanceOption{ID: "calm", Label: "Calm", Description: "Measured transitions with gentle pacing"}},
	{Option: AppearanceOption{ID: "brisk", Label: "Brisk", Description: "Faster feedback for high-volume work"}, Overrides: map[string]string{
		"motion.duration.fast": "80ms", "motion.duration.normal": "120ms", "motion.duration.slow": "180ms",
	}},
}

var typefacePresets = []appearancePreset{
	{Option: AppearanceOption{ID: "humanist", Label: "Humanist", Description: "Warm, highly readable system typography"}},
	{Option: AppearanceOption{ID: "modern", Label: "Modern", Description: "Clean geometry with an operational tone"}, Overrides: map[string]string{
		"typography.font.sans": `Aptos,"Segoe UI Variable","Segoe UI",system-ui,sans-serif`,
	}},
	{Option: AppearanceOption{ID: "classic", Label: "Classic", Description: "Editorial character for a heritage brand"}, Overrides: map[string]string{
		"typography.font.sans": `Georgia,"Times New Roman",serif`,
	}},
}

var navigationPresets = []appearancePreset{
	{Option: AppearanceOption{ID: "light", Label: "Light", Description: "Quiet navigation on a neutral surface"}},
	{Option: AppearanceOption{ID: "tinted", Label: "Tinted", Description: "A gentle brand wash behind navigation"}},
	{Option: AppearanceOption{ID: "brand", Label: "Brand", Description: "High-impact navigation in the primary color"}},
}

var colorModePresets = []appearancePreset{
	{Option: AppearanceOption{ID: "system", Label: "Use system setting", Description: "Follow this device and update automatically", Swatches: []string{"#ffffff", "#101820"}}},
	{Option: AppearanceOption{ID: "light", Label: "Light", Description: "Use the light workspace for everyone", Swatches: []string{"#ffffff", "#eaf3ef"}}},
	{Option: AppearanceOption{ID: "dark", Label: "Dark", Description: "Use the dark workspace for everyone", Swatches: []string{"#101820", "#70b7a5"}}},
}

// DefaultCustomerTheme is deliberately explicit so stored versions remain
// understandable even when additional presets are introduced later.
func DefaultCustomerTheme() CustomerTheme {
	return CustomerTheme{
		BrandName: "Human Capital Management Suite", BrandMark: "H", Palette: "evergreen", Shape: "balanced",
		ColorMode: "system", Density: "comfortable", Glyphs: "rounded-line", Typeface: "humanist", Navigation: "light", Motion: "calm",
	}
}

// NormalizeCustomerTheme refuses unknown identifiers by replacing only that
// dimension with its platform default. Corrupt or older browser state can
// therefore never select undeclared CSS behavior.
func NormalizeCustomerTheme(theme CustomerTheme) CustomerTheme {
	defaults := DefaultCustomerTheme()
	theme.BrandName = normalizedBrandText(theme.BrandName, 40, defaults.BrandName, false)
	theme.BrandMark = normalizedBrandText(theme.BrandMark, 3, defaults.BrandMark, true)
	theme.BrandLogoURL = normalizedBrandLogoURL(theme.BrandLogoURL)
	if !hasAppearancePreset(colorModePresets, theme.ColorMode) {
		theme.ColorMode = defaults.ColorMode
	}
	if !hasAppearancePreset(palettePresets, theme.Palette) {
		theme.Palette = defaults.Palette
	}
	if !hasAppearancePreset(shapePresets, theme.Shape) {
		theme.Shape = defaults.Shape
	}
	if !hasAppearancePreset(densityPresets, theme.Density) {
		theme.Density = defaults.Density
	}
	if !hasAppearancePreset(glyphPresets, theme.Glyphs) {
		theme.Glyphs = defaults.Glyphs
	}
	if !hasAppearancePreset(motionPresets, theme.Motion) {
		theme.Motion = defaults.Motion
	}
	if !hasAppearancePreset(typefacePresets, theme.Typeface) {
		theme.Typeface = defaults.Typeface
	}
	if !hasAppearancePreset(navigationPresets, theme.Navigation) {
		theme.Navigation = defaults.Navigation
	}
	return theme
}

func PaletteOptions() []AppearanceOption    { return appearanceOptions(palettePresets) }
func ColorModeOptions() []AppearanceOption  { return appearanceOptions(colorModePresets) }
func ShapeOptions() []AppearanceOption      { return appearanceOptions(shapePresets) }
func DensityOptions() []AppearanceOption    { return appearanceOptions(densityPresets) }
func GlyphOptions() []AppearanceOption      { return appearanceOptions(glyphPresets) }
func MotionOptions() []AppearanceOption     { return appearanceOptions(motionPresets) }
func TypefaceOptions() []AppearanceOption   { return appearanceOptions(typefacePresets) }
func NavigationOptions() []AppearanceOption { return appearanceOptions(navigationPresets) }

func appearanceOptions(presets []appearancePreset) []AppearanceOption {
	result := make([]AppearanceOption, 0, len(presets))
	for _, preset := range presets {
		option := preset.Option
		option.Swatches = append([]string(nil), option.Swatches...)
		result = append(result, option)
	}
	return result
}

func hasAppearancePreset(presets []appearancePreset, id string) bool {
	for _, preset := range presets {
		if preset.Option.ID == id {
			return true
		}
	}
	return false
}

// CustomerThemeAttributes is the only browser application contract. The WASM
// client writes these fixed attributes onto the root element; all resulting
// CSS was compiled into the CSP-pinned product stylesheet by the server.
func CustomerThemeAttributes(theme CustomerTheme) map[string]string {
	theme = NormalizeCustomerTheme(theme)
	return map[string]string{
		"data-hcm-color-mode": theme.ColorMode,
		"data-hcm-palette":    theme.Palette,
		"data-hcm-shape":      theme.Shape,
		"data-hcm-density":    theme.Density,
		"data-hcm-glyphs":     theme.Glyphs,
		"data-hcm-motion":     theme.Motion,
		"data-hcm-typeface":   theme.Typeface,
		"data-hcm-navigation": theme.Navigation,
	}
}

func customerThemeStylesheet() string {
	return buildTypedSheet(declareCustomerThemeStyles)
}

func normalizedBrandText(value string, limit int, fallback string, mark bool) string {
	runes := make([]rune, 0, limit)
	for _, character := range []rune(strings.TrimSpace(value)) {
		if unicode.IsControl(character) || unicode.In(character, unicode.Cf) || mark && !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '&') {
			continue
		}
		runes = append(runes, character)
		if len(runes) == limit {
			break
		}
	}
	result := strings.TrimSpace(string(runes))
	if result == "" {
		return fallback
	}
	return result
}

// normalizedBrandLogoURL keeps customer branding inside the authenticated,
// allowlisted workspace asset route. It deliberately rejects remote URLs,
// traversal, escaping, and nested paths: the asset handler admits one
// explicitly registered file name, so the theme model must not imply a wider
// fetch surface than the server actually provides.
func normalizedBrandLogoURL(value string) string {
	value = strings.TrimSpace(value)
	const prefix = "/workspace/assets/"
	if value == "" || len(value) > 240 || !strings.HasPrefix(value, prefix) || strings.ContainsAny(value, `\\?#%`) {
		return ""
	}
	name := strings.TrimPrefix(value, prefix)
	if name == "" || strings.Contains(name, "/") || path.Base(name) != name || path.Clean(name) != name {
		return ""
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".svg", ".png", ".jpg", ".jpeg", ".webp":
		return prefix + name
	default:
		return ""
	}
}
