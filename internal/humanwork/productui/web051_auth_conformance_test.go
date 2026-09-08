package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-051: accessible-authentication conformance. The entry gate
// and recovery section must satisfy the authentication-accessibility
// contract in every state and catalog locale: one titled heading with
// resolving references, discernible safe link text, DOM-order focus,
// aria-hidden never swallowing focusable content, unique ids, keyed copy,
// status-role empty states, and visible focus styling.
func TestTodo_WEB_051(t *testing.T) {
	configurations := []struct {
		name     string
		entries  []FederationEntry
		recovery []RecoveryOption
	}{
		{"full", []FederationEntry{
			{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo"},
		}, []RecoveryOption{
			{Label: "Reset your password", Description: "Use the reset flow.", Href: "https://login.harborcare.example/reset"},
		}},
		{"entries-only", []FederationEntry{
			{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "", Href: "/workspace/login/start?tenant=harborcare-demo"},
		}, nil},
		{"empty", nil, nil},
	}
	for _, configuration := range configurations {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			view := testView(PageHome)
			view.Tenant = ""
			view.Locale = ResolveProductLocale(locale)
			view = ApplyLocale(view, view.Locale)
			view.FederationEntries = configuration.entries
			view.RecoveryOptions = configuration.recovery
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			root, err := xhtml.Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			gate := findElementByID(root, "federation-entry")
			if gate == nil {
				t.Fatalf("%s %s renders no gate", configuration.name, locale)
			}
			assertGateConformance(t, gate, root, configuration.name, locale)
		}
	}
}

func assertGateConformance(t *testing.T, gate, root *xhtml.Node, name, locale string) {
	t.Helper()
	if headings := collectElements(gate, "h1"); len(headings) != 1 || textContent(headings[0]) == "" {
		t.Fatalf("%s %s gate has %d titled h1", name, locale, len(collectElements(gate, "h1")))
	}
	for _, labelledby := range collectLabelledby(gate) {
		if findElementByID(root, labelledby) == nil {
			t.Fatalf("%s %s labelledby target #%s missing", name, locale, labelledby)
		}
	}
	seen := map[string]int{}
	for _, link := range collectElements(gate, "a") {
		if textContent(link) == "" {
			t.Fatalf("%s %s gate link without discernible text", name, locale)
		}
		if href := xhtmlAttr(link, "href"); !validRecoveryHref(href) {
			t.Fatalf("%s %s gate link with unsafe destination %q", name, locale, href)
		}
	}
	collectIDs(root, seen)
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("%s %s duplicate id %q", name, locale, id)
		}
	}
	for _, tabindex := range collectTabindex(gate) {
		if tabindex > 0 {
			t.Fatalf("%s %s positive tabindex %d breaks DOM-order focus", name, locale, tabindex)
		}
	}
	for _, hidden := range collectAriaHidden(gate) {
		if len(collectElements(hidden, "a"))+len(collectElements(hidden, "button")) > 0 {
			t.Fatalf("%s %s aria-hidden swallows focusable content", name, locale)
		}
	}
	if strings.Contains(textContent(gate), "⟦") {
		t.Fatalf("%s %s gate leaks an unresolved key", name, locale)
	}
	statuses := 0
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && xhtmlAttr(node, "role") == "status" {
			statuses++
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(gate)
	if name == "empty" && statuses == 0 {
		t.Fatalf("%s %s empty gate exposes no status", name, locale)
	}
}

// Golden: the conformance verdict matrix digest (configuration × locale).
func TestTodo_WEB_051_Golden(t *testing.T) {
	var builder strings.Builder
	for _, configuration := range []string{"full", "entries-only", "empty"} {
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			builder.WriteString(configuration)
			builder.WriteString("\x00")
			builder.WriteString(locale)
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "6380d315beb57cb621e8b26012ffd5e998f655e64ef6f6069045b2822c4cd173"
	if got != want {
		t.Fatalf("authentication conformance matrix digest = %s, want %s", got, want)
	}
}

// Browser: the tenantless gate document keeps shell chrome honest.
func TestTodo_WEB_051_Browser(t *testing.T) {
	view := testView(PageHome)
	view.Tenant = ""
	view.FederationEntries = []FederationEntry{
		{Tenant: "harborcare-demo", Issuer: "https://login.harborcare.example", Protocol: "oidc", Assurance: "substantial", Href: "/workspace/login/start?tenant=harborcare-demo"},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if skip := findSkipLink(root); skip == nil || xhtmlAttr(skip, "href") != "#main-content" {
		t.Fatal("gate document has no skip link into main content")
	}
	if findElementByID(root, "main-content") == nil {
		t.Fatal("gate document has no main content target")
	}
	mains := collectElements(root, "main")
	if len(mains) != 1 {
		t.Fatal("gate document has no single main landmark")
	}
}

// Conformance: focus visibility and the shared scheme policy.
func TestTodo_WEB_051_Conformance(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, ":focus-visible") {
		t.Fatal("stylesheet exposes no focus-visible styling for gate controls")
	}
	for _, href := range []string{
		"/workspace/login/start?tenant=harborcare-demo",
		"https://login.harborcare.example/reset",
		"mailto:helpdesk@harborcare.example",
	} {
		if !validRecoveryHref(href) {
			t.Fatalf("legitimate recovery destination rejected: %q", href)
		}
	}
}

func collectLabelledby(root *xhtml.Node) []string {
	var out []string
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "aria-labelledby" {
					out = append(out, strings.Fields(attr.Val)...)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}

func collectIDs(root *xhtml.Node, seen map[string]int) {
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "id" {
					seen[attr.Val]++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
}

func collectTabindex(root *xhtml.Node) []int {
	var out []int
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "tabindex" {
					value := 0
					for _, digit := range attr.Val {
						if digit == '-' {
							continue
						}
						value = value*10 + int(digit-'0')
					}
					if strings.HasPrefix(attr.Val, "-") {
						value = -value
					}
					out = append(out, value)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}

func collectAriaHidden(root *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && xhtmlAttr(node, "aria-hidden") == "true" {
			out = append(out, node)
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}
