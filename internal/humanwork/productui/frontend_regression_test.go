package productui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestFrontendUnitEveryPageRendersInEverySupportedLocale is deliberately
// registry-driven: adding a production route adds its unit coverage without
// relying on a maintainer to remember a second page list.
func TestFrontendUnitEveryPageRendersInEverySupportedLocale(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		for _, code := range SupportedProductLocales() {
			code := code
			t.Run(string(definition.ID)+"/"+code, func(t *testing.T) {
				view := ApplyLocale(testView(definition.ID), ResolveProductLocale(code))
				doc, err := Render(view)
				if err != nil {
					t.Fatal(err)
				}
				locale := ResolveProductLocale(code)
				for _, want := range []string{
					`<html lang="` + locale.Resolved + `" dir="` + string(locale.Direction) + `"`,
					`data-hcm-catalog="product-ui.v1"`,
					`data-hcm-page-title="` + escapeTitle(view.Title) + `"`,
					`id="workspace-navigation"`,
					`id="main-content"`,
				} {
					if !strings.Contains(doc, want) {
						t.Errorf("document missing %q", want)
					}
				}
				if strings.Contains(doc, "⟦") {
					t.Error("document exposed an unresolved message key")
				}
			})
		}
	}
}

// TestFrontendRegressionEveryPageSupportsNetworkLifecycle protects the
// browser's three observable states. Cold loads must show a shaped proxy;
// warm refreshes must retain the resolved component tree; ready pages must
// remain deterministic.
func TestFrontendRegressionEveryPageSupportsNetworkLifecycle(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		t.Run(string(definition.ID), func(t *testing.T) {
			view := testView(definition.ID)
			ready, err := ui.RenderToString(Build(view))
			if err != nil {
				t.Fatal(err)
			}
			loading, err := ui.RenderToString(BuildLoading(view))
			if err != nil {
				t.Fatal(err)
			}
			refreshing, err := ui.RenderToString(BuildRefreshing(view))
			if err != nil {
				t.Fatal(err)
			}

			for state, markup := range map[string]string{"ready": ready, "loading": loading, "refreshing": refreshing} {
				if strings.Count(markup, `id="main-content"`) != 1 {
					t.Errorf("%s state has %d main-content targets, want one", state, strings.Count(markup, `id="main-content"`))
				}
				if !strings.Contains(markup, `id="workspace-navigation"`) {
					t.Errorf("%s state dropped the stable navigation shell", state)
				}
			}
			for _, want := range []string{`aria-busy="true"`, `aria-live="polite"`, `class="loading-progress"`} {
				if !strings.Contains(loading, want) {
					t.Errorf("cold-loading state missing %q", want)
				}
			}
			for _, want := range []string{`aria-busy="true"`, `data-network-state="refreshing"`, escapeTitle(view.Title)} {
				if !strings.Contains(refreshing, want) {
					t.Errorf("warm-refresh state missing %q", want)
				}
			}
			if strings.Contains(refreshing, "loading-proxy") {
				t.Error("warm refresh replaced resolved content with a cold-loading proxy")
			}
			again, err := ui.RenderToString(Build(view))
			if err != nil {
				t.Fatal(err)
			}
			if ready != again {
				t.Error("ready component tree is not deterministic")
			}
		})
	}
}

// TestFrontendRegressionAllProductLinksResolve prevents a reusable component
// from shipping a stale page path. Query state may vary, but every link and
// form action within the product subtree must resolve through the canonical
// registry used by the server and WASM history router.
func TestFrontendRegressionAllProductLinksResolve(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		t.Run(string(definition.ID), func(t *testing.T) {
			doc, err := Render(testView(definition.ID))
			if err != nil {
				t.Fatal(err)
			}
			root, err := xhtml.Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			walkElements(root, func(node *xhtml.Node) {
				for _, attribute := range []string{"href", "action"} {
					raw := attr(node, attribute)
					if raw == "" {
						continue
					}
					parsed, parseErr := url.Parse(raw)
					if parseErr != nil {
						t.Errorf("<%s> has invalid %s %q: %v", node.Data, attribute, raw, parseErr)
						continue
					}
					if !strings.HasPrefix(parsed.Path, "/workspace/app/") {
						continue
					}
					if _, ok := LookupRoute(parsed.Path); !ok {
						t.Errorf("<%s> %s %q does not resolve through the product page registry", node.Data, attribute, raw)
					}
				}
			})
		})
	}
}
