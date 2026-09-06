package productui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Render creates the deterministic server-rendered document for an already
// authorized presentation model. Feature pages never construct document
// chrome, and the shell never owns domain presentation logic.
func Render(view View) (string, error) {
	body, err := ui.RenderToString(Build(view))
	if err != nil {
		return "", fmt.Errorf("productui: render: %w", err)
	}
	return document(view.Title, view.Appearance, view.Accessibility, view.Locale, body), nil
}

// Build returns the same component tree used by SSR tests and the browser
// WASM client.
func Build(view View) ui.Node {
	page, err := renderPage(view)
	if err != nil {
		page = unavailablePanel("Page unavailable", err.Error())
	}
	return appShell(view, page)
}

// BuildEmbedded composes feature-owned content into the product shell. It is
// used by rich first-class modules that already own their page heading, such
// as the live journey state machine, and prevents duplicate document h1s.
func BuildEmbedded(view View, content ui.Node) ui.Node {
	return appShellWithHeading(view, content, false)
}

func document(title string, appearance CustomerTheme, accessibility AccessibilityPreferences, locale LocaleContext, body string) string {
	appearance = NormalizeCustomerTheme(appearance)
	accessibility = NormalizeAccessibilityPreferences(accessibility)
	locale = locale.normalized()
	attributes := CustomerThemeAttributes(appearance)
	root := `<html lang="` + escapeTitle(locale.Resolved) + `" dir="` + escapeTitle(string(locale.Direction)) + `" data-hcm-locale="` + escapeTitle(locale.Resolved) + `" data-hcm-catalog="` + escapeTitle(locale.CatalogVersion) + `" data-hcm-page-title="` + escapeTitle(title) + `"`
	if locale.Fallback != LocaleFallbackNone {
		root += ` data-hcm-locale-fallback="` + escapeTitle(string(locale.Fallback)) + `" data-hcm-requested-locale="` + escapeTitle(locale.Requested) + `"`
	}
	if missing := MissingProductTranslations(locale.Resolved); len(missing) > 0 {
		root += ` data-hcm-message-fallback="` + DefaultProductLocale + `" data-hcm-message-fallback-count="` + strconv.Itoa(len(missing)) + `"`
	}
	for _, name := range []string{"data-hcm-color-mode", "data-hcm-palette", "data-hcm-shape", "data-hcm-density", "data-hcm-glyphs", "data-hcm-typeface", "data-hcm-navigation", "data-hcm-motion"} {
		root += ` ` + name + `="` + escapeTitle(attributes[name]) + `"`
	}
	accessibilityAttributes := AccessibilityPreferenceAttributes(accessibility)
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		root += ` ` + name + `="` + escapeTitle(accessibilityAttributes[name]) + `"`
	}
	root += `>`
	brand := escapeTitle(appearance.BrandName)
	return "<!doctype html>" + root + "<head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><meta name=\"color-scheme\" content=\"light dark\"><meta name=\"application-name\" content=\"" + brand + "\"><title>" + escapeTitle(title) + " · " + brand + "</title><style>" + Stylesheet() + "</style></head><body>" + body + "</body></html>"
}

func escapeTitle(value string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;")
	return r.Replace(value)
}
