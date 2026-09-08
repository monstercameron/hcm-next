package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_WEB_019(t *testing.T) {
	indicator := VisibleFocusIndicator()
	if indicator.Selector == "" || indicator.ColorVariable != "--hcm-color-focus" {
		t.Fatalf("visible-focus contract is incomplete: %+v", indicator)
	}
	if indicator.RingWidth != "2px" || indicator.SeparatorWidth != "2px" || indicator.RingOffset != "4px" {
		t.Fatalf("visible-focus geometry changed: %+v", indicator)
	}
	if !strings.Contains(indicator.Selector, `[contenteditable="true"]`) || !strings.Contains(indicator.Selector, `[tabindex]:not([tabindex="-1"])`) {
		t.Fatalf("visible-focus selector does not distinguish editable and programmatic targets: %q", indicator.Selector)
	}

	css := Stylesheet()
	for _, want := range []string{
		"--hcm-focus-ring-width:2px",
		"--hcm-focus-ring-gap:2px",
		"--hcm-focus-ring-offset:4px",
		indicator.Selector + "{box-shadow:0 0 0 var(--hcm-focus-ring-gap) var(--surface);outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus)",
		"box-shadow:0 0 0 var(--hcm-focus-ring-gap) var(--surface)",
		".wordmark:focus-visible{box-shadow:none;outline-offset:calc(var(--hcm-focus-ring-offset) * -1);}",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet missing visible-focus contract %q", want)
		}
	}

	custom, err := StylesheetForTheme(map[string]string{
		"color.brand.primary": "#7a1f5c",
		"color.brand.hover":   "#5a1241",
		"color.brand.soft":    "#f7eaf1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(custom, "--hcm-color-brand-primary:#7a1f5c") || !strings.Contains(custom, "var(--hcm-color-focus)") {
		t.Fatal("customer theme bypassed the platform-owned focus color")
	}
}

func TestTodo_WEB_019_Golden(t *testing.T) {
	first := Stylesheet()
	second := Stylesheet()
	if first != second {
		t.Fatal("visible-focus stylesheet is not deterministic")
	}
	if strings.Count(first, "--hcm-focus-ring-width:2px") != 1 || strings.Count(first, "--hcm-focus-ring-offset:4px") != 1 {
		t.Fatal("focus geometry tokens were emitted more than once")
	}
}

func TestTodo_WEB_019_Browser(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		VisibleFocusIndicator().Selector,
		"@media (forced-colors:active)",
		"outline:2px solid Highlight!important",
		"forced-color-adjust:auto",
		"--jn-focus-color:var(--hcm-color-focus)",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("rendered product document missing browser focus contract %q", want)
		}
	}
}

func TestTodo_WEB_019_Conformance(t *testing.T) {
	if _, err := ResolveTheme(map[string]string{"color.focus": "#ffffff"}); err == nil {
		t.Fatal("customer theme was allowed to override platform focus color")
	}
	css := Stylesheet()
	for _, forbidden := range []string{
		":focus-visible{outline:none",
		":focus-visible{outline:0",
		"@media (forced-colors:active){:focus-visible{outline:none",
	} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("visible focus was disabled by %q", forbidden)
		}
	}
	for _, required := range []string{
		"@media (prefers-reduced-motion:reduce)",
		"animation:none!important",
		"transition:none!important",
		"box-shadow:0 0 0 2px Canvas!important",
		".wordmark:focus-visible{box-shadow:none!important;outline-offset:-4px!important;}",
	} {
		if !strings.Contains(css, required) {
			t.Fatalf("focus safety boundary missing %q", required)
		}
	}
}

// BenchmarkFocusRendering measures the focused control's SSR leaf composition.
// Focus is painted by the platform stylesheet in the browser, so this keeps
// the benchmark scoped to the reusable control markup and its style boundary.
func BenchmarkFocusRendering(b *testing.B) {
	control := html.Button(html.Props{
		Class: "button primary",
		Raw:   map[string]any{"data-focus-visible": "true"},
	}, ui.Text("Review request"))
	css := Stylesheet()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(control); err != nil {
			b.Fatal(err)
		}
		if !strings.Contains(css, "--hcm-focus-ring-width") {
			b.Fatal("focus stylesheet boundary disappeared")
		}
	}
}
