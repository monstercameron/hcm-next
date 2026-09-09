package journey_test

import (
	"context"
	"sync"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type preferenceSpy struct {
	mu           sync.Mutex
	tenant       values.TenantId
	organization string
	principal    string
	snapshot     preferences.Snapshot
	themeSaves   int
}

func (s *preferenceSpy) capture(tenant values.TenantId, organization, principal string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenant, s.organization, s.principal = tenant, organization, principal
}
func (s *preferenceSpy) captureUser(tenant values.TenantId, principal string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenant, s.principal = tenant, principal
}
func (s *preferenceSpy) Load(_ context.Context, tenant values.TenantId, organization, principal string) (preferences.Snapshot, error) {
	s.capture(tenant, organization, principal)
	return s.snapshot, nil
}
func (s *preferenceSpy) SaveUser(_ context.Context, tenant values.TenantId, principal string, value preferences.User) (preferences.User, error) {
	s.captureUser(tenant, principal)
	value.Version++
	s.snapshot.User = value
	return value, nil
}
func (s *preferenceSpy) SaveTheme(_ context.Context, tenant values.TenantId, organization, principal string, value preferences.TenantTheme) (preferences.TenantTheme, error) {
	s.capture(tenant, organization, principal)
	s.mu.Lock()
	value.OrganizationScopeID = organization
	value.Version++
	s.snapshot.Theme = value
	s.themeSaves++
	s.mu.Unlock()
	return value, nil
}
func (s *preferenceSpy) SaveOrganizationVisibility(_ context.Context, tenant values.TenantId, organization, principal string, value preferences.OrganizationVisibility) (preferences.OrganizationVisibility, error) {
	s.capture(tenant, organization, principal)
	s.mu.Lock()
	value.OrganizationScopeID = organization
	value.Version++
	s.snapshot.OrganizationVisibility = value
	s.mu.Unlock()
	return value, nil
}
func (s *preferenceSpy) RecordWorkflowUse(_ context.Context, tenant values.TenantId, principal, workflow string) (preferences.User, error) {
	s.captureUser(tenant, principal)
	value := preferences.NormalizeUser(s.snapshot.User)
	value.WorkflowUses[workflow]++
	value.Version++
	s.snapshot.User = value
	return value, nil
}

func TestPreferenceRPCsDeriveIdentityAndEnforceOrganizationAppearanceRole(t *testing.T) {
	spy := &preferenceSpy{snapshot: preferences.DefaultSnapshot()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Preferences: spy}))
	ctx := testContext(t)

	loaded, err := client.GetProductPreferences(ctx, &journeyv1.GetProductPreferencesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetUser().GetTables()["people"].GetPageSize() != 20 {
		t.Fatalf("defaults not projected: %+v", loaded.GetUser())
	}

	saved, err := client.SaveUserPreferences(ctx, &journeyv1.SaveUserPreferencesRequest{User: &journeyv1.UserPreferences{Locale: "de-DE", Tables: map[string]*journeyv1.TablePreferences{"people": {PageSize: 50}}}})
	if err != nil || saved.GetUser().GetVersion() != 1 {
		t.Fatalf("save user: %+v err=%v", saved, err)
	}
	spy.mu.Lock()
	tenant, organization, principal := spy.tenant, spy.organization, spy.principal
	spy.mu.Unlock()
	if tenant.String() == "" || organization == "" || principal == "" {
		t.Fatalf("identity was not derived from trusted context: tenant=%q organization=%q principal=%q", tenant, organization, principal)
	}

	_, err = client.SaveTenantAppearance(ctx, &journeyv1.SaveTenantAppearanceRequest{Theme: &journeyv1.CustomerTheme{Palette: "ocean"}})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("theme save code=%v err=%v", status.Code(err), err)
	}
	spy.mu.Lock()
	calls := spy.themeSaves
	spy.mu.Unlock()
	if calls != 0 {
		t.Fatal("unauthorized organization appearance reached the store")
	}

	adminCtx := withToken(context.Background(), fixtureAppearanceAdminToken)
	savedTheme, err := client.SaveTenantAppearance(adminCtx, &journeyv1.SaveTenantAppearanceRequest{Theme: &journeyv1.CustomerTheme{Palette: "ocean"}})
	if err != nil || savedTheme.GetTheme().GetVersion() != 1 {
		t.Fatalf("authorized appearance save: %+v err=%v", savedTheme, err)
	}
	spy.mu.Lock()
	tenant, organization, principal, calls = spy.tenant, spy.organization, spy.principal, spy.themeSaves
	spy.mu.Unlock()
	if tenant.String() != fixtureTenant || organization != fixtureOrganization || principal != "appearance-admin" || calls != 1 {
		t.Fatalf("appearance scope was not derived from trusted admin: tenant=%q organization=%q principal=%q calls=%d", tenant, organization, principal, calls)
	}

	used, err := client.RecordWorkflowUse(ctx, &journeyv1.RecordWorkflowUseRequest{WorkflowId: "promotion"})
	if err != nil || used.GetUser().GetWorkflowUses()["promotion"] != 1 {
		t.Fatalf("record workflow use: %+v err=%v", used, err)
	}
}
