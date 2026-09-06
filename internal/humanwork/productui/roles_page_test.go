package productui

import (
	"strings"
	"testing"
)

func TestRolesPageComposesCatalogCreationAndEmployeeAssignments(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{
		{ID: "manager", Name: "People manager", Description: "Manages a team", System: true, Active: true},
		{ID: "recruiter", Name: "Recruiter", Description: "Supports hiring", Active: true},
	}
	view.People = []Person{{ID: "worker-1", Name: "Priya Patel", Initials: "PP", Role: "Engineer", Team: "Product"}}
	view.RoleAssignments = []WorkerRoleAssignment{{Version: 2, WorkerRef: "worker-1", RoleIDs: []string{"manager", "recruiter"}}}
	view.RolePagePermissions = []RolePagePermission{{Version: 3, RoleID: "manager", Page: PageInsights, View: true}}
	view.EffectivePermissions = []RolePagePermission{{Page: PageRoles, View: true, Create: true, Update: true}}
	view.SaveAccessRole = func(AccessRole) {}
	view.SaveWorkerRoleAssignment = func(WorkerRoleAssignment) {}
	view.SaveRolePagePermission = func(RolePagePermission) {}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Roles and employee access", "Role catalog", "Create a role", "Priya Patel", "People manager", "Recruiter", `name="role"`, "Save employee roles", "Page and action access", "Insights", "View", "Create", "Update", "Delete"} {
		if !strings.Contains(doc, want) {
			t.Errorf("roles page missing %q", want)
		}
	}
}

func TestRolesPageHidesMutatingAffordancesForReadOnlyViewer(t *testing.T) {
	view := testView(PageRoles)
	view.AccessRoles = []AccessRole{{ID: "worker_self", Name: "Employee self-service", System: true, Active: true}}
	view.RolePagePermissions = []RolePagePermission{{RoleID: "worker_self", Page: PageInsights, View: true}}
	view.EffectivePermissions = []RolePagePermission{{Page: PageRoles, View: true}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Role catalog is read-only", "View only", `disabled`} {
		if !strings.Contains(doc, want) {
			t.Errorf("read-only roles page missing %q", want)
		}
	}
	for _, forbidden := range []string{">Create role<", ">Save employee roles<"} {
		if strings.Contains(doc, forbidden) {
			t.Errorf("read-only roles page exposes %q", forbidden)
		}
	}
}
