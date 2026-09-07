package page

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/hcm-next/tools/uxqual/floorplan"
	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	"github.com/monstercameron/hcm-next/tools/uxqual/tokens"
)

func responsiveResolution(t testing.TB) floorplan.Resolution {
	t.Helper()
	fp := minimalFloorplan()
	fp.Regions[4].Layout = floorplan.LayoutConstraint{Mode: floorplan.LayoutGrid, MinColumns: 1, MaxColumns: 3, Gap: "space.2"}
	fp.ResponsiveRules = []floorplan.ResponsiveRule{
		{Breakpoint: floorplan.BreakpointCompact, Region: "primary", Mode: floorplan.LayoutGrid, Columns: 1, Stacked: true},
		{Breakpoint: floorplan.BreakpointStandard, Region: "primary", Mode: floorplan.LayoutGrid, Columns: 2},
		{Breakpoint: floorplan.BreakpointWide, Region: "primary", Mode: floorplan.LayoutGrid, Columns: 3},
	}
	reg, err := floorplan.NewRegistry(fp)
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Resolve(minimalPageDefinition())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestTodo_WEB_023_Browser verifies the exact browser-facing serialization
// contract: one well-formed DOM contains every breakpoint projection, once,
// and the shared stylesheet selects those projections with a narrow-first
// min-width cascade. Computed layout remains a separate real-browser gate.
func TestTodo_WEB_023_Browser(t *testing.T) {
	out := renderString(t, responsiveResolution(t), minimalWidgetRegistry())
	root, err := xhtml.Parse(strings.NewReader("<body>" + out + "</body>"))
	if err != nil {
		t.Fatal(err)
	}

	primary := elementByID(root, "main-content")
	if primary == nil {
		t.Fatal("responsive render has no primary landmark")
	}
	attrs := attributes(primary)
	for key, want := range map[string]string{
		"class":                        "layout-region",
		"data-layout-narrow-mode":      "grid",
		"data-layout-narrow-columns":   "1",
		"data-layout-narrow-stacked":   "true",
		"data-layout-compact-mode":     "grid",
		"data-layout-compact-columns":  "1",
		"data-layout-compact-stacked":  "true",
		"data-layout-standard-mode":    "grid",
		"data-layout-standard-columns": "2",
		"data-layout-standard-stacked": "false",
		"data-layout-wide-mode":        "grid",
		"data-layout-wide-columns":     "3",
		"data-layout-wide-stacked":     "false",
	} {
		if attrs[key] != want {
			t.Errorf("primary %s = %q, want %q", key, attrs[key], want)
		}
	}

	css := tokens.WorkspaceCSS()
	for _, want := range []string{
		`.layout-region>:where(h1,h2,h3,h4,h5,h6){grid-column:1/-1}`,
		`.layout-region[data-layout-narrow-mode="grid"]{display:grid}`,
		`@media (min-width:40rem){`,
		`@media (min-width:60rem){`,
		`@media (min-width:80rem){`,
		`[data-layout-wide-columns="12"]{grid-template-columns:repeat(12,minmax(0,1fr))}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("renderer stylesheet missing %q", want)
		}
	}
	if strings.Contains(css, "@media (max-width:") {
		t.Error("responsive cascade contains a max-width media query")
	}
}

func TestResponsiveRendererKeepsDocumentAndActionOrder(t *testing.T) {
	res := responsiveResolution(t)
	reg := NewRegistry()
	if err := reg.Register("widget.first.v1", func(WidgetContext) ui.Node {
		return html.Span(html.Props{ID: "first"}, ui.Text("first widget"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("widget.second.v1", func(WidgetContext) ui.Node {
		return html.Div(html.Props{},
			html.Button(html.Props{Type: "button", ID: "save-action"}, ui.Text("Save")),
			html.Button(html.Props{Type: "button", ID: "submit-action"}, ui.Text("Submit")),
		)
	}); err != nil {
		t.Fatal(err)
	}
	out := renderString(t, res, reg)
	ordered := []string{`id="region-shell"`, `id="region-identity"`, `id="region-authority"`, `id="region-nav"`, `id="main-content"`, `id="region-supporting"`, `id="region-utility"`, `id="region-completion"`}
	last := -1
	for _, marker := range ordered {
		if strings.Count(out, marker) != 1 {
			t.Fatalf("landmark %s is missing or duplicated", marker)
		}
		at := strings.Index(out, marker)
		if at <= last {
			t.Fatalf("responsive metadata changed document order at %s", marker)
		}
		last = at
	}
	if save, submit := strings.Index(out, `id="save-action"`), strings.Index(out, `id="submit-action"`); save < 0 || submit <= save {
		t.Fatalf("actions were lost, duplicated, or reordered: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "display:none") || strings.Contains(out, ` hidden`) {
		t.Fatal("responsive renderer hid content instead of reflowing it")
	}
}

func TestResponsiveRendererSecurityAndClosedAttributes(t *testing.T) {
	res := responsiveResolution(t)
	res.Floorplan.Regions[4].Name = `primary" onmouseover="alert(1)`
	if _, err := Render(res, minimalWidgetRegistry()); err == nil {
		t.Fatal("Render accepted an unvalidated floorplan mutation")
	}

	out := renderString(t, responsiveResolution(t), minimalWidgetRegistry())
	attribute := regexp.MustCompile(`^data-layout-(?:narrow|compact|standard|wide)-(?:mode|columns|stacked)="(?:flow|grid|true|false|[1-9]|1[0-2])"$`)
	for _, match := range regexp.MustCompile(`data-layout-[^ =]+="[^"]*"`).FindAllString(out, -1) {
		if !attribute.MatchString(match) {
			t.Errorf("responsive renderer emitted an attribute outside the fixed vocabulary: %s", match)
		}
	}
	for _, forbidden := range []string{"style=", "onmouseover", "permission", "authority-ref", "policy-ref", "user-agent", "viewport"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("responsive projection leaked or accepted %q", forbidden)
		}
	}
}

func TestResponsiveDenseTablesUseAccessibleScrollViewport(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, want := range []string{
		`.table-scroll{overflow-x:auto;overscroll-behavior-inline:contain;scrollbar-width:thin;touch-action:pan-x pan-y}`,
		`.table-scroll table{inline-size:max-content;min-inline-size:100%;table-layout:auto;border-collapse:collapse}`,
		`.table-scroll :where(th,td){min-inline-size:8rem;overflow-wrap:normal;word-break:normal;white-space:nowrap}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("dense-table responsive CSS missing %q", want)
		}
	}
	for _, forbidden := range []string{"table-layout:fixed", ".widget-slot :where(th,td){overflow-wrap:anywhere}"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("dense-table CSS still contains readability-breaking rule %q", forbidden)
		}
	}

	fpReg := floorplan.PromotionRegistry()
	widgets := PromotionWidgetRegistry()
	for _, tc := range []struct {
		name string
		page func() pagedef.PageDefinition
		ids  []string
	}{
		{"list", pagedef.PromotionListPageDefinition, []string{"workforce-table"}},
		{"detail", pagedef.PromotionDetailPageDefinition, []string{"comparison-table", "work-items-table"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := fpReg.Resolve(tc.page())
			if err != nil {
				t.Fatal(err)
			}
			out := renderString(t, res, widgets)
			root, err := xhtml.Parse(strings.NewReader("<body>" + out + "</body>"))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range tc.ids {
				viewport := elementByID(root, id)
				if viewport == nil {
					t.Fatalf("missing scroll viewport %q", id)
				}
				attrs := attributes(viewport)
				cueID := id + "-scroll-cue"
				if attrs["class"] != "table-scroll" || attrs["role"] != "region" || attrs["tabindex"] != "0" || attrs["aria-describedby"] != cueID {
					t.Errorf("scroll viewport %q has inaccessible attributes: %+v", id, attrs)
				}
				if attrs["aria-label"] == "" || elementByID(root, cueID) == nil || firstDescendant(viewport, "table") == nil {
					t.Errorf("scroll viewport %q lost its label, cue, or semantic table", id)
				}
			}
		})
	}
}

func TestResponsiveRendererLatencyGate(t *testing.T) {
	res := responsiveResolution(t)
	reg := minimalWidgetRegistry()
	durations := make([]time.Duration, 200)
	for i := range durations {
		start := time.Now()
		node, err := Render(res, reg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ui.RenderToString(node); err != nil {
			t.Fatal(err)
		}
		durations[i] = time.Since(start)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)*95)/100]
	if p95 > 16*time.Millisecond {
		t.Fatalf("responsive page rendering p95 = %s, exceeding 16ms interaction budget", p95)
	}
}

func BenchmarkResponsivePageRendering(b *testing.B) {
	res := responsiveResolution(b)
	reg := minimalWidgetRegistry()
	b.ReportAllocs()
	for b.Loop() {
		node, err := Render(res, reg)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := ui.RenderToString(node); err != nil {
			b.Fatal(err)
		}
	}
}

func elementByID(root *xhtml.Node, id string) *xhtml.Node {
	if root.Type == xhtml.ElementNode {
		for _, attr := range root.Attr {
			if attr.Key == "id" && attr.Val == id {
				return root
			}
		}
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := elementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func attributes(node *xhtml.Node) map[string]string {
	out := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		out[attr.Key] = attr.Val
	}
	return out
}

func firstDescendant(root *xhtml.Node, tag string) *xhtml.Node {
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && child.Data == tag {
			return child
		}
		if found := firstDescendant(child, tag); found != nil {
			return found
		}
	}
	return nil
}
