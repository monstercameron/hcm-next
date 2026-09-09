package page

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/floorplan"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// minimalPageDefinition mirrors tools/uxqual/ssrshell's own
// validMinimalPage fixture (WEB-025's own test fixture) so the two
// renderers are proven against the same shape of input. It carries one
// widget slot per region that can hold one, in a fixed declaration order,
// so [Render]'s "definition order" guarantee is checkable.
func minimalPageDefinition() pagedef.PageDefinition {
	return pagedef.PageDefinition{
		PageID:       "test.page",
		Version:      1,
		FloorplanRef: "floorplan.test.v1",
		Regions: []pagedef.Region{
			{ID: "shell", Kind: pagedef.RegionShell},
			{ID: "identity", Kind: pagedef.RegionPageIdentity, Heading: &pagedef.Heading{Level: 1, Text: "Test page"}},
			{ID: "authority", Kind: pagedef.RegionAuthorityContext, Heading: &pagedef.Heading{Level: 2, Text: "Authority"}},
			{ID: "nav", Kind: pagedef.RegionLocalNavigation, Heading: &pagedef.Heading{Level: 2, Text: "Navigation"}},
			{
				ID:      "primary",
				Kind:    pagedef.RegionPrimary,
				Heading: &pagedef.Heading{Level: 2, Text: "Primary job"},
				Widgets: []pagedef.WidgetSlot{
					{ID: "w1", WidgetRef: "widget.first.v1"},
					{ID: "w2", WidgetRef: "widget.second.v1"},
				},
				Bindings: []pagedef.DataBinding{
					{ID: "b1", RPC: pagedef.RPCRef(pagedef.JourneyServiceName, "ListJourneys")},
				},
				Actions: []pagedef.ActionRef{
					{ID: "a1", RPC: pagedef.RPCRef(pagedef.JourneyServiceName, "ProposeJourney"), RequiredRole: "manager"},
				},
			},
			{ID: "supporting", Kind: pagedef.RegionSupporting, Heading: &pagedef.Heading{Level: 3, Text: "Supporting"}},
			{ID: "utility", Kind: pagedef.RegionUtility, Heading: &pagedef.Heading{Level: 3, Text: "Utility"}},
			{ID: "completion", Kind: pagedef.RegionCompletion, Heading: &pagedef.Heading{Level: 2, Text: "Completion"}},
		},
		Accessibility: pagedef.Accessibility{
			Landmarks:  []string{"banner", "navigation", "main", "complementary", "contentinfo"},
			LiveRegion: pagedef.LiveRegionPolite,
		},
		BrandTokens: []string{"brand.color.primary"},
	}
}

func minimalFloorplan() floorplan.Floorplan {
	flow := floorplan.LayoutConstraint{Mode: floorplan.LayoutFlow, MinColumns: 1, MaxColumns: 1, Gap: "space.2"}
	region := func(name string, kind pagedef.RegionKind) floorplan.Region {
		return floorplan.Region{Name: name, Kind: kind, Layout: flow}
	}
	return floorplan.Floorplan{
		ID:          "floorplan.test",
		Version:     1,
		Breakpoints: floorplan.Breakpoints(),
		Regions: []floorplan.Region{
			region("shell", pagedef.RegionShell),
			region("identity", pagedef.RegionPageIdentity),
			region("authority", pagedef.RegionAuthorityContext),
			region("nav", pagedef.RegionLocalNavigation),
			region("primary", pagedef.RegionPrimary),
			region("supporting", pagedef.RegionSupporting),
			region("utility", pagedef.RegionUtility),
			region("completion", pagedef.RegionCompletion),
		},
	}
}

func minimalResolution(t *testing.T) floorplan.Resolution {
	t.Helper()
	reg, err := floorplan.NewRegistry(minimalFloorplan())
	if err != nil {
		t.Fatalf("floorplan.NewRegistry: %v", err)
	}
	res, err := reg.Resolve(minimalPageDefinition())
	if err != nil {
		t.Fatalf("floorplan.Registry.Resolve: %v", err)
	}
	return res
}

func minimalWidgetRegistry() *Registry {
	reg := NewRegistry()
	must := func(ref string, w Widget) {
		if err := reg.Register(ref, w); err != nil {
			panic(err)
		}
	}
	must("widget.first.v1", func(ctx WidgetContext) ui.Node {
		return html.Span(html.Props{ID: "first"}, ui.Text("first widget"))
	})
	must("widget.second.v1", func(ctx WidgetContext) ui.Node {
		return html.Span(html.Props{ID: "second"}, ui.Text("second widget"))
	})
	return reg
}

func renderString(t *testing.T, res floorplan.Resolution, reg *Registry) string {
	t.Helper()
	node, err := Render(res, reg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("ui.RenderToString: %v", err)
	}
	return out
}

// TestTodo_WEB_026 is the PRIMARY test: Render builds a component tree that
// carries every region's landmark, every heading, and every registered
// widget's real content -- never an empty placeholder -- for a validated
// PageDefinition resolved against its floorplan.
func TestTodo_WEB_026(t *testing.T) {
	out := renderString(t, minimalResolution(t), minimalWidgetRegistry())

	for _, want := range []string{
		`id="page-test.page"`,
		`data-page-version="1"`,
		`id="heading-identity"`,
		`id="heading-primary"`,
		`id="first"`,
		`first widget`,
		`id="second"`,
		`second widget`,
		`data-slot-id="w1"`,
		`data-widget-ref="widget.first.v1"`,
		`data-slot-id="w2"`,
		`data-widget-ref="widget.second.v1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered tree does not contain %q\n--- got ---\n%s", want, out)
		}
	}

	// The primary region's landmark carries the well-known skip-link target
	// id, matching tools/uxqual/ssrshell's own convention.
	if !strings.Contains(out, `id="main-content"`) {
		t.Errorf("rendered tree has no main-content id on the Primary region's landmark\n%s", out)
	}
}

// TestTodo_WEB_026_Conformance proves definition order: the two widgets in
// the fixture's Primary region render in the order the PageDefinition
// declares them, not any other order (e.g. registration order, which here
// is deliberately the reverse).
func TestTodo_WEB_026_Conformance(t *testing.T) {
	reg := NewRegistry()
	must := func(ref string, text string) {
		if err := reg.Register(ref, func(WidgetContext) ui.Node { return ui.Text(text) }); err != nil {
			t.Fatalf("Register(%q): %v", ref, err)
		}
	}
	// Registered in the REVERSE of declaration order, on purpose: if Render
	// ever iterated the registry instead of the PageDefinition's own
	// Widgets slice, this would flip the two markers' positions in the
	// output.
	must("widget.second.v1", "SECOND-MARKER")
	must("widget.first.v1", "FIRST-MARKER")

	out := renderString(t, minimalResolution(t), reg)
	firstAt := strings.Index(out, "FIRST-MARKER")
	secondAt := strings.Index(out, "SECOND-MARKER")
	if firstAt < 0 || secondAt < 0 {
		t.Fatalf("both markers must appear in the output: %s", out)
	}
	if firstAt >= secondAt {
		t.Errorf("widget.first.v1 (slot w1) rendered after widget.second.v1 (slot w2); PageDefinition declaration order was not preserved\n%s", out)
	}
}

// TestTodo_WEB_026_Fault proves Render refuses an unregistered widget ref
// with a typed, wrapped error rather than silently skipping the slot or
// emitting free HTML for it.
func TestTodo_WEB_026_Fault(t *testing.T) {
	res := minimalResolution(t)
	reg := NewRegistry() // deliberately empty: neither slot's ref resolves

	node, err := Render(res, reg)
	if err == nil {
		t.Fatalf("Render with an empty registry succeeded, want a refusal")
	}
	if node != nil {
		t.Fatalf("Render returned a non-nil node alongside an error")
	}
	var unregistered *UnregisteredWidgetError
	if !errors.As(err, &unregistered) {
		t.Fatalf("Render error = %v (%T), want *UnregisteredWidgetError", err, err)
	}
	if unregistered.WidgetRef != "widget.first.v1" {
		t.Errorf("UnregisteredWidgetError.WidgetRef = %q, want %q (the first unresolved slot in definition order)", unregistered.WidgetRef, "widget.first.v1")
	}
	if !errors.Is(err, ErrUnregisteredWidget) {
		t.Errorf("errors.Is(err, ErrUnregisteredWidget) = false, want true")
	}

	// An invalid PageDefinition is refused before any widget is even
	// looked up: Render's other stated refusal path.
	invalid := res
	invalid.Page.Regions = nil
	if _, err := Render(invalid, minimalWidgetRegistry()); err == nil {
		t.Fatal("Render of a PageDefinition with no regions succeeded, want a refusal")
	}
}

// TestTodo_WEB_026_Security proves the defense-in-depth this package shares
// with tools/uxqual/ssrshell: even calling buildPage directly (bypassing
// [pagedef.PageDefinition.Validate], which [Render] always applies first),
// GWC's own SSR writer still escapes text and attribute content rather than
// emitting it raw, so a mutated field can never break out of its element or
// inject a new attribute.
func TestTodo_WEB_026_Security(t *testing.T) {
	pd := minimalPageDefinition()
	pd.Regions[1].Heading.Text = `<script>alert(1)</script>`
	pd.Regions[4].Widgets[0].WidgetRef = `widget.first.v1" onmouseover="alert(1)`

	// Validate refuses the script-tag mutation outright -- the primary
	// defense.
	if violations := pd.Validate(); len(violations) == 0 {
		t.Fatal("pagedef.Validate accepted a heading containing a <script> tag")
	}

	// The registry must still resolve by the mutated ref for buildPage to
	// reach the point where it writes that ref into an attribute at all.
	reg := NewRegistry()
	if err := reg.Register(`widget.first.v1" onmouseover="alert(1)`, func(WidgetContext) ui.Node {
		return ui.Text("content")
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register("widget.second.v1", func(WidgetContext) ui.Node { return ui.Text("content") }); err != nil {
		t.Fatalf("Register: %v", err)
	}

	node, err := buildPage(pd, reg)
	if err != nil {
		t.Fatalf("buildPage (bypassing Validate): %v", err)
	}
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("ui.RenderToString: %v", err)
	}

	if strings.Contains(out, "<script>") {
		t.Errorf("rendered tree contains a raw, executable <script> element:\n%s", out)
	}
	root, err := xhtml.Parse(strings.NewReader("<body>" + out + "</body>"))
	if err != nil {
		t.Fatalf("rendered fragment does not parse as HTML: %v", err)
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "onmouseover", "onerror", "onclick", "onload":
					t.Errorf("rendered tree has an injected event-handler attribute %s=%q on <%s>", a.Key, a.Val, n.Data)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

// TestTodo_WEB_026_Integration renders the two real Promotion
// PageDefinitions (tools/uxqual/pagedef) resolved against the real Gate A
// floorplan registry (tools/uxqual/floorplan.PromotionRegistry) using the
// real widget registry this package registers from
// tools/uxqual/render/journey ([PromotionWidgetRegistry]) -- proving the
// whole WEB-002/WEB-003/WEB-026 pipeline composes end to end, not just this
// package's own unit fixtures.
func TestTodo_WEB_026_Integration(t *testing.T) {
	fpReg := floorplan.PromotionRegistry()
	widgets := PromotionWidgetRegistry()

	for _, ref := range []string{"widget.first.v1"} {
		if _, ok := widgets.Lookup(ref); ok {
			t.Fatalf("PromotionWidgetRegistry unexpectedly resolves the test-only ref %q", ref)
		}
	}

	t.Run("list", func(t *testing.T) {
		res, err := fpReg.Resolve(pagedef.PromotionListPageDefinition())
		if err != nil {
			t.Fatalf("floorplan.Registry.Resolve: %v", err)
		}
		out := renderString(t, res, widgets)
		for _, want := range []string{
			`id="page-promotion.journeys.list"`,
			"Omar Reyes", // journey.SampleListPage's journeys list, via widget.list.journeys.v1
			"Workforce",  // the region heading text
		} {
			if !strings.Contains(out, want) {
				t.Errorf("rendered Promotion list page does not contain %q", want)
			}
		}
	})

	t.Run("detail", func(t *testing.T) {
		res, err := fpReg.Resolve(pagedef.PromotionDetailPageDefinition())
		if err != nil {
			t.Fatalf("floorplan.Registry.Resolve: %v", err)
		}
		out := renderString(t, res, widgets)
		for _, want := range []string{
			`id="page-promotion.journeys.detail"`,
			"Awaiting approval", // journey.SampleDetailPage's active step, via widget.stepper.journey-stage.v1
		} {
			if !strings.Contains(out, want) {
				t.Errorf("rendered Promotion detail page does not contain %q", want)
			}
		}
	})
}

// TestTodo_WEB_026_Browser parses the rendered tree with the same HTML5
// parser (golang.org/x/net/html) tools/uxqual/ssrshell's own BROWSER matrix
// test uses, and asserts on the resulting DOM rather than on markup
// substrings: every landmark this package's fixture declares is a genuinely
// distinct, well-formed element in document order, and no element carries a
// "javascript:" URI a browser would treat as executable.
//
// As with tools/uxqual/ssrshell's own documented precedent, a real-browser
// (Chromium/AT) pass is not exercised here: that gap belongs to whatever
// later lane mounts this tree into a served, browser-driven route.
func TestTodo_WEB_026_Browser(t *testing.T) {
	out := renderString(t, minimalResolution(t), minimalWidgetRegistry())

	root, err := xhtml.Parse(strings.NewReader("<body>" + out + "</body>"))
	if err != nil {
		t.Fatalf("rendered tree does not parse as HTML: %v", err)
	}

	wantTags := []string{"header", "section", "section", "nav", "main", "aside", "aside", "footer"}
	gotTags := landmarkTagsInOrder(root)
	if len(gotTags) != len(wantTags) {
		t.Fatalf("landmark tags in DOM order = %v, want %v", gotTags, wantTags)
	}
	for i := range wantTags {
		if gotTags[i] != wantTags[i] {
			t.Errorf("landmark[%d] = %q, want %q (full: got=%v want=%v)", i, gotTags[i], wantTags[i], gotTags, wantTags)
		}
	}

	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				if strings.Contains(strings.ToLower(a.Val), "javascript:") {
					t.Errorf("element <%s> attribute %s=%q carries a javascript: URI", n.Data, a.Key, a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

// landmarkTagsInOrder returns the tag name of every header/nav/main/aside/
// footer/section element found under root, in document order.
func landmarkTagsInOrder(root *xhtml.Node) []string {
	landmarks := map[string]bool{"header": true, "nav": true, "main": true, "aside": true, "footer": true, "section": true}
	var out []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && landmarks[n.Data] {
			out = append(out, n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}
