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
	for _, want := range []string{`role="list"`, `data-organization-view="tree"`, `org_view=flat`, "Avery Patel"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree view missing %q", want)
		}
	}
}

func TestOrganizationReportingDisclosuresAndAmbiguousNames(t *testing.T) {
	view := testView(PageOrganization)
	view.People = []Person{
		{ID: "a", Name: "Alex", WorkerNumber: "W01"},
		{ID: "b", Name: "Alex", WorkerNumber: "W02"},
		{ID: "c", Name: "Chris", Manager: "Alex"},
		{ID: "d", Name: "Dana", Manager: "Chris"},
	}
	nodes := ownershipTree(view)
	if len(nodes) != 3 || len(nodes[0].Reports) != 0 || len(nodes[1].Reports) != 0 || len(nodes[2].Reports) != 1 {
		t.Fatalf("ambiguous manager must not receive an arbitrary report: %+v", nodes)
	}
	doc, err := Render(ApplyRequest(view, PageRequest{OrganizationView: "tree"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="ownership-reports"`, "Direct reports: 1", "W01", "W02", `aria-current="true"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, absent := range []string{`role="tree"`, `role="treeitem"`, "Unavailable · Unavailable"} {
		if strings.Contains(doc, absent) {
			t.Errorf("invalid tree output %q", absent)
		}
	}
}

func TestOrganizationCountLabelsFollowLocale(t *testing.T) {
	for locale, label := range map[string]string{"en-US": "People:", "de-DE": "Personen:", "ar": "الأشخاص:"} {
		t.Run(locale, func(t *testing.T) {
			view := ApplyRequest(testView(PageOrganization), PageRequest{Locale: locale})
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(doc, label) || strings.Contains(doc, "visible workers") {
				t.Fatalf("counts do not follow locale %s", locale)
			}
			if strings.Index(doc, view.Locale.Text("organization.structure_title")) > strings.Index(doc, view.Locale.Text("organization.metadata_title")) {
				t.Fatal("workforce exploration must precede business metadata")
			}
		})
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
