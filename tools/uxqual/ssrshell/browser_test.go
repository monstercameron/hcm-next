package ssrshell

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// TestTodo_WEB_025_Browser is the BROWSER matrix test. Unlike
// tools/uxqual/pagedef's own documented skip (a Go value contract has no
// rendering surface at all), this package's output IS a document a browser
// parses and exposes through its accessibility tree, so this test parses
// the real rendered output with the same HTML5 parser
// (golang.org/x/net/html) tools/uxqual/qual uses for its own document
// checks, and asserts on the resulting DOM: the skip link is the first
// focusable element and targets a real element id, every landmark this
// package documents is a genuinely distinct, well-formed element in
// document order, and no element carries a "javascript:" URI a browser
// would treat as executable.
//
// A real-browser (Chromium/AT) pass is not exercised here, matching
// tools/uxqual/forms's own FORM-004 Browser test precedent: that gap
// belongs to whatever later lane wires this renderer's output into a
// served route and can drive it with an actual browser.
func TestTodo_WEB_025_Browser(t *testing.T) {
	pd := validMinimalPage()
	rs, err := Render(pd)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	root, err := html.Parse(strings.NewReader(rs.HTML))
	if err != nil {
		t.Fatalf("rendered document does not parse as HTML: %v", err)
	}

	t.Run("the skip link is the first focusable element and targets a real element id", func(t *testing.T) {
		firstAnchor := findFirst(root, atom.A)
		if firstAnchor == nil {
			t.Fatalf("no <a> element found in the document")
		}
		href := attrOf(firstAnchor, "href")
		if href != "#main-content" {
			t.Fatalf("first <a> href = %q, want \"#main-content\"", href)
		}
		if findByID(root, "main-content") == nil {
			t.Fatalf("no element with id=\"main-content\" exists for the skip link to target")
		}
	})

	t.Run("every documented landmark tag for this page's regions is a distinct element in document order", func(t *testing.T) {
		var wantTags []string
		for _, r := range pd.Regions {
			tag, _, ok := LandmarkForRegionKind(r.Kind)
			if !ok {
				t.Fatalf("fixture region %q has undocumented kind %q", r.ID, r.Kind)
			}
			wantTags = append(wantTags, tag)
		}
		gotTags := landmarkTagsInOrder(root)
		if len(gotTags) != len(wantTags) {
			t.Fatalf("landmark tags in DOM order = %v, want %v", gotTags, wantTags)
		}
		for i := range wantTags {
			if gotTags[i] != wantTags[i] {
				t.Errorf("landmark[%d] = %q, want %q (full: got=%v want=%v)", i, gotTags[i], wantTags[i], gotTags, wantTags)
			}
		}
	})

	t.Run("no element in the DOM carries a javascript: URI", func(t *testing.T) {
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
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
	})

	t.Run("the live region is reachable in the DOM with the declared aria-live value", func(t *testing.T) {
		lr := findByID(root, "live-region")
		if lr == nil {
			t.Fatalf("no element with id=\"live-region\" found")
		}
		if got := attrOf(lr, "aria-live"); got != "polite" {
			t.Errorf("live-region aria-live = %q, want %q", got, "polite")
		}
	})
}

func findFirst(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, a); found != nil {
			return found
		}
	}
	return nil
}

func findByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode && attrOf(n, "id") == id {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByID(c, id); found != nil {
			return found
		}
	}
	return nil
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// landmarkTagsInOrder returns the tag name of every header/nav/main/aside/
// footer/section element found under <body>, in document order.
func landmarkTagsInOrder(root *html.Node) []string {
	landmarkAtoms := map[atom.Atom]bool{
		atom.Header:  true,
		atom.Nav:     true,
		atom.Main:    true,
		atom.Aside:   true,
		atom.Footer:  true,
		atom.Section: true,
	}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && landmarkAtoms[n.DataAtom] {
			out = append(out, n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}
