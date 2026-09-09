package journey_test

import (
	"context"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workerIDStoreSpy struct {
	policy              workerids.Policy
	tenant              values.TenantId
	organization, actor string
}

func (s *workerIDStoreSpy) Load(_ context.Context, tenant values.TenantId, organization string) (workerids.Policy, error) {
	s.tenant, s.organization = tenant, organization
	return s.policy, nil
}
func (s *workerIDStoreSpy) Save(_ context.Context, tenant values.TenantId, organization, actor string, p workerids.Policy) (workerids.Policy, error) {
	s.tenant, s.organization, s.actor = tenant, organization, actor
	p.Version++
	s.policy = p
	return p, nil
}
func (*workerIDStoreSpy) Reserve(context.Context, values.TenantId, string, string, workerids.FormatContext) (string, error) {
	return "HC-001000", nil
}

func TestWorkerIDPolicyRPCIsOrganizationScopedAndAdminOnly(t *testing.T) {
	p := workerids.DefaultPolicy()
	spy := &workerIDStoreSpy{policy: p}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{WorkerIDs: spy, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }}))
	if _, err := client.GetWorkerIDPolicy(testContext(t), &journeyv1.GetWorkerIDPolicyRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ordinary user code=%s err=%v", status.Code(err), err)
	}
	ctx := withToken(context.Background(), fixtureAppearanceAdminToken)
	loaded, err := client.GetWorkerIDPolicy(ctx, &journeyv1.GetWorkerIDPolicyRequest{})
	if err != nil || len(loaded.GetPreviews()) != 4 || loaded.GetPreviews()[0] != "HC-001000" {
		t.Fatalf("load=%+v err=%v", loaded, err)
	}
	request := loaded.GetPolicy()
	request.Prefix = "NW"
	saved, err := client.SaveWorkerIDPolicy(ctx, &journeyv1.SaveWorkerIDPolicyRequest{Policy: request})
	if err != nil || saved.GetPolicy().GetVersion() != 1 {
		t.Fatalf("save=%+v err=%v", saved, err)
	}
	if spy.tenant.String() != fixtureTenant || spy.organization != fixtureOrganization || spy.actor != "appearance-admin" {
		t.Fatalf("untrusted scope reached store: %+v", spy)
	}
}
