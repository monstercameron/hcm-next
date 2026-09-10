package productui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestUXBLIND_023(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{
		{ID: "manager", Name: "Manager", Active: true},
		{ID: "support", Name: "Support", Active: true},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{
		{RoleID: "manager", Mode: "OWN_UNIT", OrganizationUnits: []string{"Engineering"}},
		{RoleID: "support", Mode: "ALL", OrganizationUnits: []string{"Platform"}},
	}
	view.People = []Person{{ID: "worker-1", Name: "Worker One", Team: "Engineering"}, {ID: "worker-2", Name: "Worker Two", Team: "Platform"}}

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	for _, roleID := range []string{"manager", "support"} {
		editor := findVisibilityEditor(root, roleID)
		if editor == nil {
			t.Fatalf("missing visibility editor for %q", roleID)
		}
		fieldset := findClassToken(editor, "organization-visibility-units")
		if fieldset == nil {
			t.Fatalf("missing unit fieldset for %q", roleID)
		}
		if !hasUXAttr(fieldset, "disabled", "") {
			t.Errorf("unit fieldset for %q is editable", roleID)
		}
		if !hasUXAttr(fieldset, "aria-disabled", "true") || !hasUXAttr(fieldset, "data-selection-state", "inactive") {
			t.Errorf("unit fieldset for %q lacks an accessible inactive state", roleID)
		}
	}

	for _, want := range []string{
		"Role grants are additive.",
		"Manager: Their own organization unit",
		"Support: Everyone",
		"Selections are used by the allowlist and denylist modes.",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("visibility document missing explanation %q", want)
		}
	}
}

func TestUXBLIND_023_AllowAndDenyModesKeepUnitSelectionAvailable(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{
		{ID: "allow", Name: "Allow", Active: true},
		{ID: "deny", Name: "Deny", Active: true},
	}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{
		{RoleID: "allow", Mode: "ALLOWLIST"},
		{RoleID: "deny", Mode: "DENYLIST"},
	}
	view.People = []Person{{ID: "worker-1", Name: "Worker One", Team: "Engineering"}}

	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, roleID := range []string{"allow", "deny"} {
		fieldset := findClassToken(findVisibilityEditor(root, roleID), "organization-visibility-units")
		if fieldset == nil {
			t.Fatalf("missing unit fieldset for %q", roleID)
		}
		if hasUXAttr(fieldset, "disabled", "") || hasUXAttr(fieldset, "data-selection-state", "inactive") {
			t.Errorf("unit fieldset for %q is inactive in a selectable mode", roleID)
		}
	}
}

func findVisibilityEditor(root *xhtml.Node, roleID string) *xhtml.Node {
	var found *xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if found != nil {
			return
		}
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "data-role-id" && attr.Val == roleID {
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

func hasUXAttr(node *xhtml.Node, key, value string) bool {
	if node == nil {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Key == key && (value == "" || attr.Val == value) {
			return true
		}
	}
	return false
}
