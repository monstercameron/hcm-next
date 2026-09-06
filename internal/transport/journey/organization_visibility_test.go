package journey_test

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/hcm-next/internal/experience/preferences"
	"github.com/monstercameron/hcm-next/internal/experience/roleaccess"
	"github.com/monstercameron/hcm-next/internal/humanwork/workspace"
	"github.com/monstercameron/hcm-next/internal/transport/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListWorkersEnforcesOrganizationVisibilityBeforeTheResponse(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: fixtureSubject, WorkerID: "self-id", PreferredName: "Jane", OrgUnit: "People", ManagerRef: "leader-hidden"},
		{WorkerRef: "peer", WorkerID: "peer-id", PreferredName: "Peer", OrgUnit: "People", ManagerRef: fixtureSubject},
		{WorkerRef: "leader-hidden", WorkerID: "leader-id", PreferredName: "Leader", OrgUnit: "Executive"},
		{WorkerRef: "finance", WorkerID: "finance-id", PreferredName: "Finance", OrgUnit: "Finance"},
	}
	engine.options.OrgUnits = []string{"People", "Executive", "Finance"}
	spy := &preferenceSpy{snapshot: preferences.DefaultSnapshot()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, Preferences: spy}))

	spy.snapshot.OrganizationVisibility = preferences.OrganizationVisibility{Mode: preferences.OrganizationVisibilityOwnUnit}
	response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 2 || response.GetWorkers()[0].GetManagerRef() != "" {
		t.Fatalf("own-unit response leaked a worker or hidden manager: %+v", response.GetWorkers())
	}
	if got := response.GetOptions().GetOrgUnits(); len(got) != 1 || got[0] != "People" {
		t.Fatalf("own-unit options leaked units: %v", got)
	}

	spy.snapshot.OrganizationVisibility = preferences.OrganizationVisibility{Mode: preferences.OrganizationVisibilityAllowlist, OrganizationUnits: []string{"Finance"}}
	response, err = client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 2 || response.GetWorkers()[0].GetWorkerRef() != fixtureSubject || response.GetWorkers()[1].GetWorkerRef() != "finance" {
		t.Fatalf("allowlist did not preserve self plus the qualified unit: %+v", response.GetWorkers())
	}

	spy.snapshot.OrganizationVisibility = preferences.OrganizationVisibility{Mode: preferences.OrganizationVisibilityDenylist, OrganizationUnits: []string{"Executive"}}
	response, err = client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 3 {
		t.Fatalf("denylist response = %+v", response.GetWorkers())
	}

	adminResponse, err := client.ListWorkers(withToken(context.Background(), fixtureAppearanceAdminToken), &journeyv1.ListWorkersRequest{})
	if err != nil || len(adminResponse.GetWorkers()) != 4 {
		t.Fatalf("administrator must retain the complete configuration population: count=%d err=%v", len(adminResponse.GetWorkers()), err)
	}
}

func TestListWorkersUsesDurableEmployeeRolesAndAdditiveRolePolicies(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: fixtureSubject, OrgUnit: "People"},
		{WorkerRef: "finance", OrgUnit: "Finance"},
		{WorkerRef: "executive", OrgUnit: "Executive"},
		{WorkerRef: "sales", OrgUnit: "Sales"},
	}
	access := &roleAccessSpy{snapshot: roleaccess.Snapshot{
		Assignments: []roleaccess.Assignment{{WorkerRef: fixtureSubject, RoleIDs: []string{"finance_partner", "executive_partner"}}},
		Policies: []roleaccess.VisibilityPolicy{
			{RoleID: "finance_partner", Mode: roleaccess.VisibilityAllowlist, OrganizationUnits: []string{"Finance"}},
			{RoleID: "executive_partner", Mode: roleaccess.VisibilityAllowlist, OrganizationUnits: []string{"Executive"}},
		},
	}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: access}))
	response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 3 || response.GetWorkers()[0].GetWorkerRef() != fixtureSubject || response.GetWorkers()[1].GetWorkerRef() != "finance" || response.GetWorkers()[2].GetWorkerRef() != "executive" {
		t.Fatalf("additive role visibility = %+v", response.GetWorkers())
	}
}

func TestListWorkersDefaultsAnUnconfiguredActiveRoleToOwnUnit(t *testing.T) {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: fixtureSubject, OrgUnit: "People"},
		{WorkerRef: "peer", OrgUnit: "People"},
		{WorkerRef: "outside", OrgUnit: "Finance"},
	}
	access := &roleAccessSpy{snapshot: roleaccess.Snapshot{Roles: roleaccess.DefaultRoles()}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: access}))
	response, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetWorkers()) != 2 {
		t.Fatalf("unconfigured active-role response leaked workers: %+v", response.GetWorkers())
	}
}

func TestOrganizationVisibilityWriteRequiresAdministrator(t *testing.T) {
	spy := &preferenceSpy{snapshot: preferences.DefaultSnapshot()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Preferences: spy}))
	request := &journeyv1.SaveOrganizationVisibilityRequest{Policy: &journeyv1.OrganizationVisibilityPolicy{Mode: preferences.OrganizationVisibilityOwnUnit}}
	if _, err := client.SaveOrganizationVisibility(testContext(t), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ordinary user save code=%v err=%v", status.Code(err), err)
	}
	for _, invalid := range []*journeyv1.SaveOrganizationVisibilityRequest{
		nil,
		{},
		{Policy: &journeyv1.OrganizationVisibilityPolicy{Mode: "unexpected"}},
	} {
		if _, err := client.SaveOrganizationVisibility(withToken(context.Background(), fixtureAppearanceAdminToken), invalid); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid administrator save code=%v err=%v", status.Code(err), err)
		}
	}
	response, err := client.SaveOrganizationVisibility(withToken(context.Background(), fixtureAppearanceAdminToken), request)
	if err != nil || response.GetPolicy().GetVersion() != 1 || response.GetPolicy().GetMode() != preferences.OrganizationVisibilityOwnUnit {
		t.Fatalf("administrator save = %+v err=%v", response, err)
	}
}
