package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-064: current and proposed organization scope. The
// role-visibility editor mutates a draft of each role's policy, but today
// neither the current nor the proposed scope is ever resolved: a DENYLIST
// never shows what it admits, an ALLOWLIST never shows what it withholds.
// Each editor must resolve both scopes — current from the policy,
// proposed from the draft — through one pure resolver that never invents
// scope: unknown modes and blank names resolve to nothing, matching is
// case-insensitive, output is sorted and de-duplicated.
func TestTodo_WEB_064(t *testing.T) {
	available := []string{"Platform", "Engineering", "Design"}
	for _, resolution := range []struct {
		mode      string
		units     []string
		available []string
		want      []string
	}{
		{"ALL", nil, available, []string{"Design", "Engineering", "Platform"}},
		{"all", nil, available, []string{"Design", "Engineering", "Platform"}},
		{"ALLOWLIST", []string{"Engineering"}, available, []string{"Engineering"}},
		{"allowlist", []string{"engineering", "ENGINEERING", " Design "}, available, []string{"Design", "Engineering"}},
		{"ALLOWLIST", []string{"Ghost"}, available, nil},
		{"ALLOWLIST", nil, available, nil},
		{"DENYLIST", []string{"Platform"}, available, []string{"Design", "Engineering"}},
		{"denylist", nil, available, []string{"Design", "Engineering", "Platform"}},
		{"OWN_UNIT", []string{"Engineering"}, available, nil},
		{"OWN_UNIT", nil, nil, nil},
		{"", []string{"Engineering"}, available, nil},
		{"SUPERUSER", []string{"Engineering"}, available, nil},
		{"ALL", nil, nil, nil},
		{"ALLOWLIST", []string{"", "  "}, available, nil},
	} {
		if got := ResolveOrganizationScope(resolution.mode, resolution.units, resolution.available); !reflect.DeepEqual(got, resolution.want) {
			t.Fatalf("ResolveOrganizationScope(%q, %q, %q) = %q, want %q", resolution.mode, resolution.units, resolution.available, got, resolution.want)
		}
	}
	before := []string{"Platform", "Engineering"}
	_ = ResolveOrganizationScope("DENYLIST", []string{"Platform"}, before)
	if !reflect.DeepEqual(before, []string{"Platform", "Engineering"}) {
		t.Fatal("scope resolution mutates its inputs")
	}

	// Each active role editor resolves its current scope from the policy:
	// the allowlist names its unit, the denylist names what it admits.
	doc, err := Render(web064View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	body := textContent(root)
	for _, want := range []string{"Current scope", "Proposed scope", "Engineering", "Design"} {
		if !strings.Contains(body, want) {
			t.Fatalf("visibility page resolves no %q", want)
		}
	}
	manager := web064RoleScope(t, root, "manager", "organization-scope-current")
	if !strings.Contains(manager, "Engineering") || strings.Contains(manager, "Platform") || strings.Contains(manager, "Design") {
		t.Fatalf("manager current scope = %q, want Engineering only", manager)
	}
	support := web064RoleScope(t, root, "support", "organization-scope-current")
	if !strings.Contains(support, "Design") || !strings.Contains(support, "Engineering") || strings.Contains(support, "Platform") {
		t.Fatalf("support current scope = %q, want Design and Engineering only", support)
	}
}

// web064RoleScope returns the text of one role editor's named scope block.
func web064RoleScope(t *testing.T, root *xhtml.Node, roleID, scopeClass string) string {
	t.Helper()
	var editor *xhtml.Node
	var find func(node *xhtml.Node)
	find = func(node *xhtml.Node) {
		if editor != nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if (attr.Key == "data-role-id" || attr.Key == "data-hcm-role-id") && attr.Val == roleID {
					editor = node
					return
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}
	find(root)
	if editor == nil {
		t.Fatalf("no editor for role %q", roleID)
	}
	block := findClassToken(editor, scopeClass)
	if block == nil {
		t.Fatalf("role %q resolves no %q block", roleID, scopeClass)
	}
	return textContent(block)
}

// findClassToken finds the first descendant (or self) whose class
// attribute contains token as a whole whitespace-separated token.
func findClassToken(root *xhtml.Node, token string) *xhtml.Node {
	var found *xhtml.Node
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "class" {
					for _, field := range strings.Fields(attr.Val) {
						if field == token {
							found = node
							return
						}
					}
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

// Golden: the scope resolution matrix digest.
func TestTodo_WEB_064_Golden(t *testing.T) {
	var builder strings.Builder
	available := []string{"Platform", "Engineering", "Design"}
	for _, mode := range []string{"ALL", "OWN_UNIT", "ALLOWLIST", "DENYLIST", "SUPERUSER", ""} {
		for _, units := range [][]string{nil, {"Engineering"}, {"engineering", "Platform"}, {"Ghost"}} {
			builder.WriteString(mode)
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(units, ","))
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(ResolveOrganizationScope(mode, units, available), ","))
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "cd8e3dbd6c28556a8ad3311e1dd0f5bfd9aa7793a6a45c2531eb06d614408457"
	if got != want {
		t.Fatalf("scope matrix digest = %s, want %s", got, want)
	}
}

// Browser: the armed visibility document carries a labelled current and
// proposed scope per active role, with no positive tabindex stops.
func TestTodo_WEB_064_Browser(t *testing.T) {
	doc, err := Render(web064View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var walkCount func(node *xhtml.Node)
	walkCount = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "class" {
					for _, field := range strings.Fields(attr.Val) {
						if field == "organization-scope" {
							count++
						}
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walkCount(child)
		}
	}
	walkCount(root)
	if count != 4 {
		t.Fatalf("visibility document resolves %d scopes, want 4 (current + proposed × 2 roles)", count)
	}
	var positive int
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
					positive++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if positive != 0 {
		t.Fatalf("visibility document carries %d positive tabindex stops", positive)
	}
}

// Conformance: scope copy in three locales, resolver determinism.
func TestTodo_WEB_064_Conformance(t *testing.T) {
	for locale, current := range map[string]string{"en-US": "Current scope", "de-DE": "Aktueller Bereich", "ar": "النطاق الحالي"} {
		lc := ResolveProductLocale(locale)
		if got := lc.Text("organization_visibility.current_scope"); got != current {
			t.Fatalf("%s current scope reads %q, want %q", locale, got, current)
		}
		if key := lc.Text("organization_visibility.proposed_scope"); strings.Contains(key, "⟦") {
			t.Fatalf("%s proposed scope unresolved: %q", locale, key)
		}
		if key := lc.Text("organization_visibility.scope_empty"); strings.Contains(key, "⟦") {
			t.Fatalf("%s scope empty unresolved: %q", locale, key)
		}
		doc, err := Render(web064View(locale))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("%s visibility render leaks an unresolved key", locale)
		}
	}
	first := ResolveOrganizationScope("DENYLIST", []string{"Platform"}, []string{"Platform", "Engineering"})
	second := ResolveOrganizationScope("DENYLIST", []string{"Platform"}, []string{"Platform", "Engineering"})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("scope resolution is nondeterministic")
	}
}

func web064View(locale string) View {
	view := testView(PageOrganizationVisibility)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.People = []Person{
		{ID: "worker-ava", Name: "Ava Reyes", Team: "Platform"},
		{ID: "worker-ivan", Name: "Ivan Petrov", Team: "Engineering"},
		{ID: "worker-dana", Name: "Dana Kim", Team: "Design"},
	}
	view.AccessRoles = []AccessRole{
		{ID: "manager", Name: "Manager", Active: true},
		{ID: "support", Name: "Support", Active: true},
		{ID: "archived", Name: "Archived", Active: false},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{
		{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering"}},
		{RoleID: "support", Mode: "DENYLIST", OrganizationUnits: []string{"Platform"}},
	}
	return view
}
