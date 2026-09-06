package productui

import (
	"strings"
	"testing"
)

func TestOrganizationVisibilityAdminPageExposesEveryPolicyModeAndUnit(t *testing.T) {
	view := testView(PageOrganizationVisibility)
	view.AccessRoles = []AccessRole{{ID: "hr_partner", Name: "HR partner", Active: true}}
	view.RoleVisibilityPolicies = []OrganizationVisibilityPolicy{{Version: 3, RoleID: "hr_partner", Mode: "ALLOWLIST", OrganizationUnits: []string{"People Operations"}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Set visibility by role", "HR partner", "Everyone", "Their own organization unit", "Only selected units", "All except selected units", "People Operations", `id="organization-visibility-status-hr_partner"`, `name="organization-visibility-mode-hr_partner"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("organization visibility page missing %q", want)
		}
	}
}
