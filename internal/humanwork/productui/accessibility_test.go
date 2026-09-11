package productui

import (
	"strconv"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestEveryRegisteredPageMeetsTheProductAccessibilityContract prevents a new
// route or reusable component from bypassing the shell's minimum semantic
// contract. Browser and assistive-technology qualification remains separate.
func TestEveryRegisteredPageMeetsTheProductAccessibilityContract(t *testing.T) {
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
			assertDocumentLanguage(t, root)
			assertPageLandmarks(t, root)
			assertInteractiveNames(t, root)
			assertImageAlternatives(t, root)
			assertHeadingOrder(t, root)
			assertARIAReferences(t, root)
			assertTableRoleHierarchy(t, root)
		})
	}
}

func assertDocumentLanguage(t *testing.T, root *xhtml.Node) {
	t.Helper()
	htmlNode := firstElement(root, "html")
	if htmlNode == nil || attr(htmlNode, "lang") == "" || attr(htmlNode, "dir") == "" {
		t.Fatal("document must declare language and direction")
	}
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		if attr(htmlNode, name) == "" {
			t.Errorf("document does not declare %s", name)
		}
	}
}

func assertPageLandmarks(t *testing.T, root *xhtml.Node) {
	t.Helper()
	if countElements(root, "main") != 1 || countElements(root, "h1") != 1 {
		t.Fatalf("page must contain exactly one main and h1; main=%d h1=%d", countElements(root, "main"), countElements(root, "h1"))
	}
	main := firstElement(root, "main")
	if attr(main, "id") != "main-content" {
		t.Fatal("main landmark must retain the skip-link target")
	}
	focusable := collectFocusable(root)
	if len(focusable) == 0 || focusable[0].Data != "a" || attr(focusable[0], "href") != "#main-content" {
		t.Fatal("skip link must be the first focusable element")
	}
}

func assertInteractiveNames(t *testing.T, root *xhtml.Node) {
	t.Helper()
	labels := map[string]bool{}
	walkElements(root, func(node *xhtml.Node) {
		if node.Data == "label" && attr(node, "for") != "" {
			labels[attr(node, "for")] = strings.TrimSpace(nodeText(node)) != ""
		}
	})
	walkElements(root, func(node *xhtml.Node) {
		if tabindex := attr(node, "tabindex"); tabindex != "" {
			value, err := strconv.Atoi(tabindex)
			if err != nil || value > 0 {
				t.Errorf("<%s> has invalid or positive tabindex %q", node.Data, tabindex)
			}
		}
		if !isInteractive(node) || attr(node, "type") == "hidden" {
			return
		}
		name := strings.TrimSpace(attr(node, "aria-label"))
		if name == "" && attr(node, "aria-labelledby") != "" {
			name = "referenced"
		}
		if name == "" && labels[attr(node, "id")] {
			name = "labelled"
		}
		if name == "" {
			name = strings.TrimSpace(nodeText(node))
		}
		if name == "" {
			t.Errorf("interactive <%s id=%q> has no accessible name", node.Data, attr(node, "id"))
		}
	})
}

func assertImageAlternatives(t *testing.T, root *xhtml.Node) {
	t.Helper()
	walkElements(root, func(node *xhtml.Node) {
		if node.Data == "img" && !hasAttr(node, "alt") {
			t.Errorf("image %q has no alt attribute", attr(node, "src"))
		}
	})
}

func assertHeadingOrder(t *testing.T, root *xhtml.Node) {
	t.Helper()
	previous := 0
	walkElements(root, func(node *xhtml.Node) {
		if len(node.Data) != 2 || node.Data[0] != 'h' || node.Data[1] < '1' || node.Data[1] > '6' {
			return
		}
		rank := int(node.Data[1] - '0')
		if previous > 0 && rank > previous+1 {
			t.Errorf("heading order skips from h%d to h%d (%q)", previous, rank, strings.TrimSpace(nodeText(node)))
		}
		previous = rank
	})
}

func assertARIAReferences(t *testing.T, root *xhtml.Node) {
	t.Helper()
	ids := map[string]bool{}
	walkElements(root, func(node *xhtml.Node) {
		if id := attr(node, "id"); id != "" {
			ids[id] = true
		}
	})
	walkElements(root, func(node *xhtml.Node) {
		for _, name := range []string{"aria-labelledby", "aria-describedby"} {
			for _, id := range strings.Fields(attr(node, name)) {
				if !ids[id] {
					t.Errorf("<%s> %s references missing id %q", node.Data, name, id)
				}
			}
		}
	})
}

func assertTableRoleHierarchy(t *testing.T, root *xhtml.Node) {
	t.Helper()
	walkElements(root, func(node *xhtml.Node) {
		role := attr(node, "role")
		if role != "row" && role != "columnheader" && role != "cell" && role != "rowgroup" {
			return
		}
		for parent := node.Parent; parent != nil; parent = parent.Parent {
			parentRole := attr(parent, "role")
			if parent.Data == "table" || parentRole == "table" || parentRole == "grid" || parentRole == "treegrid" {
				return
			}
		}
		t.Errorf("role=%q has no table/grid ancestor", role)
	})
}

func collectFocusable(root *xhtml.Node) []*xhtml.Node {
	result := []*xhtml.Node{}
	walkElements(root, func(node *xhtml.Node) {
		if isInteractive(node) && attr(node, "type") != "hidden" && attr(node, "aria-disabled") != "true" {
			result = append(result, node)
		}
	})
	return result
}

func isInteractive(node *xhtml.Node) bool {
	switch node.Data {
	case "button", "input", "select", "textarea", "summary":
		return true
	case "a":
		return attr(node, "href") != ""
	default:
		return attr(node, "tabindex") != "" && attr(node, "tabindex") != "-1"
	}
}

func firstElement(root *xhtml.Node, name string) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && node.Data == name {
			found = node
		}
	})
	return found
}

func countElements(root *xhtml.Node, name string) int {
	count := 0
	walkElements(root, func(node *xhtml.Node) {
		if node.Data == name {
			count++
		}
	})
	return count
}

func walkElements(root *xhtml.Node, visit func(*xhtml.Node)) {
	if root.Type == xhtml.ElementNode {
		visit(root)
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walkElements(child, visit)
	}
}

func attr(node *xhtml.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func hasAttr(node *xhtml.Node, name string) bool {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return true
		}
	}
	return false
}

func nodeText(node *xhtml.Node) string {
	if attr(node, "aria-hidden") == "true" {
		return ""
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(current *xhtml.Node) {
		if current.Type == xhtml.TextNode {
			b.WriteString(current.Data)
			b.WriteByte(' ')
		}
		if current != node && attr(current, "aria-hidden") == "true" {
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(b.String()), " ")
}
