package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-048: prove shell keyboard and landmark navigation. The shell
// must resolve a complete named-landmark inventory for every page, expose a
// working skip link into labelled main content, keep every dialog trigger
// linkage and name, and close its dialogs on Escape like the search and
// launcher surfaces do.
func TestTodo_WEB_048(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	landmarks := ResolveShellLandmarks(view)
	want := []ShellLandmark{
		{Role: "complementary", Label: "Workspace navigation"},
		{Role: "navigation", Label: "Main"},
		{Role: "navigation", Label: "Support and preferences"},
		{Role: "navigation", Label: "Breadcrumbs"},
		{Role: "main", Label: "Roles & access"},
	}
	if !reflectDeepEqualLandmarks(landmarks, want) {
		t.Fatalf("roles landmarks = %#v, want %#v", landmarks, want)
	}

	// A top-level page has no trail landmark but keeps the rest. Support
	// navigation resolves only behind a projection.
	home := ResolveShellLandmarks(testView(PagePeople))
	for _, landmark := range home {
		if landmark.Role == "navigation" && landmark.Label == "Breadcrumbs" {
			t.Fatalf("people landmarks advertise no trail: %#v", home)
		}
	}
	if len(home) != 3 {
		t.Fatalf("people landmarks = %#v, want 3 without support or breadcrumbs", home)
	}
	projected := ResolveShellLandmarks(ApplyRoleVisibility(testView(PagePeople), []string{"manager"}))
	if len(projected) != 4 {
		t.Fatalf("projected people landmarks = %#v, want 4 with support", projected)
	}

	// Unknown pages resolve the Home inventory, never an empty one.
	unknown := ResolveShellLandmarks(View{Page: PageID("no-such-page"), Locale: ResolveProductLocale("")})
	if len(unknown) == 0 || unknown[len(unknown)-1].Role != "main" {
		t.Fatalf("unknown page landmarks = %#v, want Home inventory", unknown)
	}
}

// Golden: the landmark inventory for one authorized nested view.
func TestTodo_WEB_048_Golden(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageRoles), []string{RoleHCMAdmin})
	var builder strings.Builder
	for _, landmark := range ResolveShellLandmarks(view) {
		builder.WriteString(landmark.Role)
		builder.WriteString("\x00")
		builder.WriteString(landmark.Label)
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "1d58b14d99e87f200e147d728cf5ff294099acb1d9716b727594e25604f8e880"
	if got != want {
		t.Fatalf("shell landmark golden digest = %s, want %s", got, want)
	}
}

// Browser: every resolved landmark exists in the document with its name,
// and the skip link targets labelled main content.
func TestTodo_WEB_048_Browser(t *testing.T) {
	for _, page := range []PageID{PageRoles, PageHistory, PagePerson} {
		roles := []string{"manager"}
		if page == PageRoles {
			roles = []string{RoleHCMAdmin}
		}
		view := ApplyRoleVisibility(testView(page), roles)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, landmark := range ResolveShellLandmarks(view) {
			if !landmarkInDocument(root, landmark, view) {
				t.Fatalf("page %s landmark missing in document: %#v", page, landmark)
			}
		}
		skip := findSkipLink(root)
		if skip == nil || xhtmlAttr(skip, "href") != "#main-content" {
			t.Fatalf("page %s has no skip link into main content", page)
		}
		if findElementByID(root, "main-content") == nil {
			t.Fatalf("page %s skip target #main-content missing", page)
		}
	}
}

// Conformance: locale-complete inventories, dialog linkage, drawer Escape parity.
func TestTodo_WEB_048_Conformance(t *testing.T) {
	for _, definition := range PageDefinitions() {
		roles := []string{"manager"}
		if !PageVisible(definition.ID, roles) {
			roles = []string{RoleHCMAdmin}
		}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			view := ApplyRoleVisibility(testView(definition.ID), roles)
			view.Locale = ResolveProductLocale(locale)
			view = ApplyLocale(view, view.Locale)
			for _, landmark := range ResolveShellLandmarks(view) {
				if landmark.Role == "" || landmark.Label == "" {
					t.Fatalf("page %s locale %s landmark unnamed: %#v", definition.ID, locale, landmark)
				}
				if strings.Contains(landmark.Label, "⟦") {
					t.Fatalf("page %s locale %s landmark leaks a key: %#v", definition.ID, locale, landmark)
				}
			}
		}
	}
	// The drawer dialog closes on Escape exactly like the search and launcher
	// inputs do: the handler lives on the dialog and flips the same state.
	if !drawerEscapeCloses("Escape") || drawerEscapeCloses("Enter") {
		t.Fatal("drawer Escape handling does not match the shell dialog contract")
	}
}

func reflectDeepEqualLandmarks(got, want []ShellLandmark) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func landmarkInDocument(root *xhtml.Node, landmark ShellLandmark, view View) bool {
	switch landmark.Role {
	case "complementary":
		aside := findAsideNavigation(root)
		return aside != nil && xhtmlAttr(aside, "aria-label") == landmark.Label
	case "navigation":
		return navWithLabel(root, landmark.Label) != nil
	case "main":
		mains := collectElements(root, "main")
		if len(mains) != 1 || xhtmlAttr(mains[0], "aria-labelledby") != "page-title" {
			return false
		}
		title := findPageTitle(findPageHead(root))
		return title != nil && textContent(title) == landmark.Label
	}
	return false
}

func findAsideNavigation(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "aside" && xhtmlAttr(node, "id") == "workspace-navigation" {
			found = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func navWithLabel(root *xhtml.Node, label string) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "nav" && xhtmlAttr(node, "aria-label") == label {
			found = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findSkipLink(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode && node.Data == "a" {
			for _, attr := range node.Attr {
				if attr.Key == "class" && strings.Contains(attr.Val, "skip-link") {
					found = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}
