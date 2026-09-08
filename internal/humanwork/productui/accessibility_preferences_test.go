package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAccessibilityPreferencesAreBoundedAndApplyAcrossTheDocument(t *testing.T) {
	value := NormalizeAccessibilityPreferences(AccessibilityPreferences{TextSize: "huge", Contrast: "unsafe", Motion: "reduce", Links: "underlined"})
	if value.TextSize != "standard" || value.Contrast != "system" || value.Motion != "reduce" || value.Links != "underlined" {
		t.Fatalf("normalized accessibility preferences = %+v", value)
	}
	view := testView(PageHome)
	view.Accessibility = AccessibilityPreferences{TextSize: "larger", Contrast: "more", Motion: "reduce", Links: "underlined"}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-hcm-text-size="larger"`, `data-hcm-contrast="more"`, `data-hcm-motion-preference="reduce"`, `data-hcm-links="underlined"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("document missing %q", want)
		}
	}
}

func TestLimitedMotionPreferenceKeepsOnlyBoundedStateCues(t *testing.T) {
	value := NormalizeAccessibilityPreferences(AccessibilityPreferences{Motion: "limited"})
	if value.Motion != "limited" {
		t.Fatalf("limited motion normalized to %q", value.Motion)
	}
	options := AccessibilityMotionOptions()
	if len(options) != 3 || options[1].ID != "limited" {
		t.Fatalf("motion options = %+v", options)
	}
	view := testView(PageHome)
	view.Accessibility = value
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `data-hcm-motion-preference="limited"`) {
		t.Fatal("limited motion preference did not reach the document token")
	}
}

func TestAccessibilityPreferenceEditorUsesLabeledNativeControls(t *testing.T) {
	props := AccessibilityPreferencesProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Value:     DefaultAccessibilityPreferences(), TextSizes: AccessibilityTextSizeOptions(),
		Contrasts: AccessibilityContrastOptions(), Motions: AccessibilityMotionOptions(), LinkStyles: AccessibilityLinkOptions(),
	}
	out, err := ui.RenderToString(AccessibilityPreferencesPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-labelledby="accessibility-title"`, `aria-describedby="accessibility-text-size-help"`,
		`type="radio"`, `name="contrast"`, `name="motion-preference"`, `name="links"`,
		`role="status"`, `aria-live="polite"`, `type="submit"`, `type="button"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("accessibility editor missing %q", want)
		}
	}
}

func TestAccessibilitySafetyCSSCannotBeReplacedByBrandChoices(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:root[data-hcm-text-size="larger"]`, `:root[data-hcm-contrast="more"]`,
		`:root[data-hcm-motion-preference="limited"]`, `:root[data-hcm-motion-preference="reduce"]`, `:root[data-hcm-links="underlined"]`,
		`@media (prefers-contrast:more)`, `@media (forced-colors:active)`, `@media (prefers-reduced-motion:reduce)`,
		`.people-row>.people-cell:before{color:var(--muted);content:attr(data-label);`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("accessibility stylesheet missing %q", want)
		}
	}
}
