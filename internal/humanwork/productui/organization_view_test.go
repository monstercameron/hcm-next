package productui

import (
	"strings"
	"testing"
)

func TestOrganizationViewCanSwitchBetweenFlatAndOwnershipTree(t *testing.T) {
	view := testView(PageOrganization)
	flat, err := Render(ApplyRequest(view, PageRequest{OrganizationView: "flat"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flat, `data-organization-view="flat"`) || !strings.Contains(flat, `org_view=tree`) {
		t.Fatalf("flat view did not expose a software-routable tree toggle")
	}
	for _, want := range []string{`class="organization-unit-disclosure"`, `<summary class="org-node manager">`, "Avery Patel", `class="organization-unit-members"`} {
		if !strings.Contains(flat, want) {
			t.Errorf("expandable flat view missing %q", want)
		}
	}
	tree, err := Render(ApplyRequest(view, PageRequest{OrganizationView: "tree"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="tree"`, `data-organization-view="tree"`, `org_view=flat`, "Avery Patel"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree view missing %q", want)
		}
	}
}

func TestOwnershipTreeDoesNotDropCyclesOrOrphans(t *testing.T) {
	view := NewView(PageOrganization, "tenant", "principal", "manager")
	view.People = []Person{
		{ID: "a", Name: "A", Manager: "B", Team: "One"},
		{ID: "b", Name: "B", Manager: "A", Team: "One"},
		{ID: "c", Name: "C", Manager: "Missing", Team: "Two"},
	}
	nodes := ownershipTree(view)
	seen := map[string]bool{}
	var walk func([]OwnershipNodeProps)
	walk = func(values []OwnershipNodeProps) {
		for _, value := range values {
			seen[value.Name] = true
			walk(value.Reports)
		}
	}
	walk(nodes)
	for _, name := range []string{"A", "B", "C"} {
		if !seen[name] {
			t.Errorf("worker %s was dropped", name)
		}
	}
}
