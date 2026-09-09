package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestComposeDevPersonasIssuesFourDistinctVerifiedIdentities(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := ServeConfig{DevBrowserLogin: true, DevHMACKey: testDevKey, Issuer: DefaultIssuer, Audience: DefaultAudience, Tenant: LocalDevTenant}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	personas := composeDevPersonas(verifier, cfg, func() time.Time { return now })
	if len(personas) != 4 {
		t.Fatalf("personas = %d, want 4", len(personas))
	}
	wantSubject := map[string]string{
		"admin": "hc-050-rafael-torres", "hiring-manager": "hc-052-dominic-collins",
		"payroll-manager": "hc-054-thomas-baker", "individual-contributor": "hc-022-samuel-rivera",
	}
	wantName := map[string]string{
		"admin": "Rafael Torres", "hiring-manager": "Dominic Collins",
		"payroll-manager": "Thomas Baker", "individual-contributor": "Samuel Rivera",
	}
	planned, err := demoworkforce.Plan(pgstore.TenantID(cfg.Tenant))
	if err != nil {
		t.Fatal(err)
	}
	plannedByKey := make(map[string]bool, len(planned))
	for _, worker := range planned {
		plannedByKey[worker.Row.WorkerKey] = worker.Row.LifecycleStatus == "active"
	}
	seen := map[string]bool{}
	for _, persona := range personas {
		principal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify %s: %v", persona.ID, verifyErr)
		}
		if principal.Subject() != wantSubject[persona.ID] {
			t.Errorf("%s subject = %q, want %q", persona.ID, principal.Subject(), wantSubject[persona.ID])
		}
		if persona.Name != wantName[persona.ID] {
			t.Errorf("%s name = %q, want %q", persona.ID, persona.Name, wantName[persona.ID])
		}
		if !plannedByKey[principal.Subject()] {
			t.Errorf("%s subject %q is not an active worker in the durable demo seed plan", persona.ID, principal.Subject())
		}
		if seen[principal.Subject()] {
			t.Errorf("duplicate subject %q", principal.Subject())
		}
		seen[principal.Subject()] = true
	}
}

func TestComposeDevPersonasStaysOffOutsideExplicitDevLogin(t *testing.T) {
	if got := composeDevPersonas(nil, ServeConfig{}, nil); len(got) != 0 {
		t.Fatalf("disabled personas = %d", len(got))
	}
}

func TestComposeDevPersonasDoesNotAdvertiseHarborCareWorkersForAnotherTenant(t *testing.T) {
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(testDevKey), Issuer: DefaultIssuer, Audience: DefaultAudience})
	if err != nil {
		t.Fatal(err)
	}
	cfg := ServeConfig{DevBrowserLogin: true, Tenant: "another-tenant", Issuer: DefaultIssuer, Audience: DefaultAudience}
	if got := composeDevPersonas(verifier, cfg, time.Now); len(got) != 0 {
		t.Fatalf("foreign tenant received %d HarborCare personas", len(got))
	}
}
