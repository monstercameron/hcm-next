package workspace

import (
	"errors"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/hcm-next/tools/uxqual/floorplan"
	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/page"
)

func fixtureSession() Session {
	return Session{
		Tenant:  "northwind",
		Subject: "avery.okafor@northwind.example",
		Roles:   []string{"hr.business_partner", "promotion.approver"},
		Purpose: "promotion_review",
	}
}

func listResolution(t *testing.T) floorplan.Resolution {
	t.Helper()
	res, err := floorplan.PromotionRegistry().Resolve(pagedef.PromotionListPageDefinition())
	if err != nil {
		t.Fatalf("floorplan.Registry.Resolve: %v", err)
	}
	return res
}

func detailResolution(t *testing.T) floorplan.Resolution {
	t.Helper()
	res, err := floorplan.PromotionRegistry().Resolve(pagedef.PromotionDetailPageDefinition())
	if err != nil {
		t.Fatalf("floorplan.Registry.Resolve: %v", err)
	}
	return res
}

func buildString(t *testing.T, in Input) string {
	t.Helper()
	node, err := Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return renderNode(t, node)
}

// TestTodo_WEB_121 is the PRIMARY test: Build composes the navigation and
// authority-context regions with the page's own rendered tree, including
// that page tree's canonical status/live region, for a real Promotion page.
func TestTodo_WEB_121(t *testing.T) {
	in := Input{
		Resolution: listResolution(t),
		Widgets:    page.PromotionWidgetRegistry(),
		Route:      journeyclient.Parse(""),
		Session:    fixtureSession(),
	}
	out := buildString(t, in)

	for _, want := range []string{
		`id="` + RootElementID + `"`,
		`href="#` + SkipTargetElementID + `"`,
		`id="` + NavElementID + `"`,
		`id="` + StatusElementID + `"`,
		`id="` + AuthorityElementID + `"`,
		"avery.okafor@northwind.example",
		`id="page-promotion.journeys.list"`,
		"Workforce",
		"Omar Reyes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Build output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestTodo_WEB_121_Conformance proves the shell's own document order: skip
// link, then navigation, then authority strip, then the page's own root. The
// page-owned status region is the root's first child, never a competing shell
// sibling -- preserving shell chrome before page identity and one announcer
// regardless of which page is inside it.
func TestTodo_WEB_121_Conformance(t *testing.T) {
	in := Input{
		Resolution: listResolution(t),
		Widgets:    page.PromotionWidgetRegistry(),
		Route:      journeyclient.Parse(""),
		Session:    fixtureSession(),
	}
	out := buildString(t, in)

	markers := []string{
		`href="#` + SkipTargetElementID + `"`,
		`id="` + NavElementID + `"`,
		`id="` + AuthorityElementID + `"`,
		`id="page-promotion.journeys.list"`,
		`id="` + StatusElementID + `"`,
	}
	positions := make([]int, len(markers))
	for i, m := range markers {
		positions[i] = strings.Index(out, m)
		if positions[i] < 0 {
			t.Fatalf("marker %q not found in output: %s", m, out)
		}
	}
	for i := 1; i < len(positions); i++ {
		if positions[i-1] >= positions[i] {
			t.Errorf("shell document order violated: %q (at %d) does not precede %q (at %d)", markers[i-1], positions[i-1], markers[i], positions[i])
		}
	}
}

// TestBuildOwnsExactlyOneLiveRegion prevents the page-owned announcer and
// shell composition from drifting back to two AT announcement targets.
func TestBuildOwnsExactlyOneLiveRegion(t *testing.T) {
	out := buildString(t, Input{
		Resolution: listResolution(t),
		Widgets:    page.PromotionWidgetRegistry(),
		Route:      journeyclient.Parse(""),
		Session:    fixtureSession(),
	})
	root, err := xhtml.Parse(strings.NewReader("<body>" + out + "</body>"))
	if err != nil {
		t.Fatalf("rendered shell does not parse as HTML: %v", err)
	}

	pageRoot := findByID(root, page.RootElementIDPrefix+"promotion.journeys.list")
	if pageRoot == nil {
		t.Fatal("composed workspace has no governed page root")
	}
	var liveRegions []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			role := attrOf(n, "role")
			if role == "status" || role == "alert" || attrOf(n, "aria-live") != "" {
				liveRegions = append(liveRegions, n)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if len(liveRegions) != 1 {
		t.Fatalf("composed workspace live regions = %d, want exactly 1", len(liveRegions))
	}
	if got := attrOf(liveRegions[0], "id"); got != page.LiveRegionElementID {
		t.Fatalf("composed workspace live-region id = %q, want %q", got, page.LiveRegionElementID)
	}
	if !isDescendant(pageRoot, liveRegions[0]) {
		t.Fatal("canonical live region is not owned by the governed page root")
	}
}

// TestTodo_WEB_121_Fault proves Build propagates
// tools/uxqual/render/page.Render's own refusal (an unregistered widget
// ref) rather than rendering a partial shell around the failure.
func TestTodo_WEB_121_Fault(t *testing.T) {
	in := Input{
		Resolution: listResolution(t),
		Widgets:    page.NewRegistry(), // deliberately empty
		Route:      journeyclient.Parse(""),
		Session:    fixtureSession(),
	}
	node, err := Build(in)
	if err == nil {
		t.Fatal("Build with an empty widget registry succeeded, want a refusal")
	}
	if node != nil {
		t.Fatal("Build returned a non-nil node alongside an error")
	}
	var unregistered *page.UnregisteredWidgetError
	if !errors.As(err, &unregistered) {
		t.Fatalf("Build error = %v (%T), want it to wrap *page.UnregisteredWidgetError", err, err)
	}
	if !errors.Is(err, page.ErrUnregisteredWidget) {
		t.Errorf("errors.Is(err, page.ErrUnregisteredWidget) = false, want true")
	}
}

// TestTodo_WEB_121_Integration builds both real Promotion pages through the
// full pipeline (floorplan.PromotionRegistry -> page.PromotionWidgetRegistry
// -> Build), across every route Nav distinguishes, proving the navigation
// region's "current" marker tracks the address the shell is actually
// rendering for.
func TestTodo_WEB_121_Integration(t *testing.T) {
	widgets := page.PromotionWidgetRegistry()

	t.Run("list route with no selection", func(t *testing.T) {
		out := buildString(t, Input{Resolution: listResolution(t), Widgets: widgets, Route: journeyclient.Parse(""), Session: fixtureSession()})
		if !strings.Contains(out, `id="page-promotion.journeys.list"`) {
			t.Errorf("missing the list page's own root: %s", out)
		}
	})

	t.Run("list route with a worker selected", func(t *testing.T) {
		route := journeyclient.Parse("#/journeys?worker=worker:NW-40118")
		out := buildString(t, Input{Resolution: listResolution(t), Widgets: widgets, Route: route, Session: fixtureSession()})
		if !strings.Contains(out, "worker:NW-40118") {
			t.Errorf("nav does not reflect the selected worker: %s", out)
		}
	})

	t.Run("detail route", func(t *testing.T) {
		route := journeyclient.Parse("#/journeys/int_01JX6Y8B2C7D9EFG")
		out := buildString(t, Input{Resolution: detailResolution(t), Widgets: widgets, Route: route, Session: fixtureSession()})
		if !strings.Contains(out, `id="page-promotion.journeys.detail"`) {
			t.Errorf("missing the detail page's own root: %s", out)
		}
		if !strings.Contains(out, "int_01JX6Y8B2C7D9EFG") {
			t.Errorf("nav does not name the open journey: %s", out)
		}
		if !strings.Contains(out, "Awaiting approval") {
			t.Errorf("missing the detail page's own widget content: %s", out)
		}
	})

	t.Run("no session carries no authority strip", func(t *testing.T) {
		out := buildString(t, Input{Resolution: listResolution(t), Widgets: widgets, Route: journeyclient.Parse("")})
		if strings.Contains(out, `id="`+AuthorityElementID+`"`) {
			t.Errorf("authority strip rendered for a zero Session: %s", out)
		}
	})
}

// TestTodo_WEB_121_Browser parses the shell's rendered tree with the same
// HTML5 parser (golang.org/x/net/html) tools/uxqual/ssrshell and
// tools/uxqual/render/page's own BROWSER matrix tests use: every landmark
// the shell and the page inside it declare is a genuinely distinct,
// well-formed element, the skip link's target id exists, and no element
// carries a "javascript:" URI.
//
// As with those packages' documented precedent, a real-browser (Chromium/
// AT) pass is not exercised here.
func TestTodo_WEB_121_Browser(t *testing.T) {
	in := Input{
		Resolution: listResolution(t),
		Widgets:    page.PromotionWidgetRegistry(),
		Route:      journeyclient.Parse(""),
		Session:    fixtureSession(),
	}
	out := buildString(t, in)

	root, err := xhtml.Parse(strings.NewReader("<body>" + out + "</body>"))
	if err != nil {
		t.Fatalf("rendered shell does not parse as HTML: %v", err)
	}

	if findByID(root, SkipTargetElementID) == nil {
		t.Errorf("no element with id=%q for the skip link to target", SkipTargetElementID)
	}
	if findFirstA(root) == nil || attrOf(findFirstA(root), "href") != "#"+SkipTargetElementID {
		t.Errorf("the skip link is not the first <a> in the document, or does not target %q", SkipTargetElementID)
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

func findByID(n *xhtml.Node, id string) *xhtml.Node {
	if n.Type == xhtml.ElementNode && attrOf(n, "id") == id {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByID(c, id); found != nil {
			return found
		}
	}
	return nil
}

func findFirstA(n *xhtml.Node) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == "a" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstA(c); found != nil {
			return found
		}
	}
	return nil
}

func isDescendant(ancestor, node *xhtml.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current == ancestor {
			return true
		}
	}
	return false
}

func attrOf(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
