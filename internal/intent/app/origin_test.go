package app

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// originPrincipal builds a verified principal for the origin tests.
func originPrincipal(t *testing.T, kind trust.SubjectKind, delegations ...string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               "acme-corp",
		Subject:              "principal:hr-partner-7",
		SubjectKind:          kind,
		OrganizationScopeID:  "org:acme-corp:engineering",
		Purposes:             []string{"promotion.annual_cycle"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session:19f2",
		DelegationRefs:       delegations,
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// TestDeriveOriginTakesEveryTrustedFieldFromTheCredential is the serving-path
// half of INTENT-012: the origin this cell records is derived entirely from the
// verified principal and the resolved invocation, the wire request has no
// origin field to select one with, and each authenticated actor kind maps onto
// exactly one trusted origin kind.
func TestDeriveOriginTakesEveryTrustedFieldFromTheCredential(t *testing.T) {
	t.Run("a human with no delegation is a HUMAN origin", func(t *testing.T) {
		p := originPrincipal(t, trust.SubjectKindHuman)
		origin, err := deriveOrigin(p, nil)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if origin.Kind != intent.OriginHuman {
			t.Fatalf("origin kind = %s, want HUMAN", origin.Kind)
		}
		if origin.Initiator.PrincipalID != p.Subject() ||
			origin.Initiator.IdentityAssuranceRef != p.EvidenceID() {
			t.Fatalf("the initiator was not taken from the credential: %+v", origin.Initiator)
		}
		if origin.Tenant != p.Tenant() || origin.OrganizationScopeID != p.OrganizationScopeID() {
			t.Fatalf("the scope was not taken from the credential: %+v", origin)
		}
		if origin.SessionRef != p.SessionRef() || origin.TrustedContextDigest != p.Fingerprint() {
			t.Fatalf("the session or trusted context was not taken from the credential: %+v", origin)
		}
		if origin.Assurance != intent.AssuranceHigh {
			t.Fatalf("assurance = %s, want HIGH", origin.Assurance)
		}
		if origin.ConfersAuthority() {
			t.Fatal("the derived origin claims to confer authority")
		}
		if err := origin.Validate(); err != nil {
			t.Fatalf("the derived origin does not revalidate: %v", err)
		}
	})

	t.Run("a human under a delegation is a DELEGATED_HUMAN origin", func(t *testing.T) {
		p := originPrincipal(t, trust.SubjectKindHuman, "delegation:vacation-cover")
		origin, err := deriveOrigin(p, nil)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if origin.Kind != intent.OriginDelegatedHuman {
			t.Fatalf("origin kind = %s, want DELEGATED_HUMAN", origin.Kind)
		}
	})

	t.Run("an agent is an AGENT origin and never a human one", func(t *testing.T) {
		p := originPrincipal(t, trust.SubjectKindAgent)
		origin, err := deriveOrigin(p, nil)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if origin.Kind != intent.OriginAgent {
			t.Fatalf("origin kind = %s, want AGENT", origin.Kind)
		}
		if origin.Initiator.Kind != intent.InitiatorAgent {
			t.Fatalf("initiator kind = %s, want AGENT", origin.Initiator.Kind)
		}
	})

	t.Run("a service or connector is a WORKLOAD origin with its own evidence", func(t *testing.T) {
		for _, kind := range []trust.SubjectKind{trust.SubjectKindService, trust.SubjectKindIntegration} {
			p := originPrincipal(t, kind)
			origin, err := deriveOrigin(p, nil)
			if err != nil {
				t.Fatalf("%s: derive: %v", kind, err)
			}
			if origin.Kind != intent.OriginWorkload {
				t.Fatalf("%s: origin kind = %s, want WORKLOAD", kind, origin.Kind)
			}
			// A connector reaching this API does not inherit its provider's
			// authority: no provider is recorded, because none authenticated.
			if origin.ProviderAuthorityRef != "" {
				t.Fatalf("%s: a synchronous call recorded a provider authority %q",
					kind, origin.ProviderAuthorityRef)
			}
			if origin.ProducerRef == "" || origin.SourceEvidenceRef == "" {
				t.Fatalf("%s: a non-interactive origin names no producer or evidence: %+v", kind, origin)
			}
		}
	})

	t.Run("the delegation grants the credential carries are recorded", func(t *testing.T) {
		p := originPrincipal(t, trust.SubjectKindHuman, "delegation:vacation-cover", "delegation:region-eu")
		origin, err := deriveOrigin(p, nil)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if len(origin.DelegationRefs) != 2 {
			t.Fatalf("delegation refs = %v, want both grants", origin.DelegationRefs)
		}
	})

	t.Run("an origin whose scope is unset is refused", func(t *testing.T) {
		now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
		p, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant:               "acme-corp",
			Subject:              "principal:hr-partner-7",
			SubjectKind:          trust.SubjectKindHuman,
			AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance:            trust.AssuranceHigh,
			SessionRef:           "session:19f2",
			IssuedAt:             now.Add(-time.Minute),
			ExpiresAt:            now.Add(time.Hour),
			CredentialDigest:     "credential-digest",
		})
		if err != nil {
			t.Fatalf("NewPrincipal: %v", err)
		}
		if _, err := deriveOrigin(p, nil); !errors.Is(err, intent.ErrUntrustedOrigin) {
			t.Fatalf("an origin with no organization scope was derived: %v", err)
		}
	})
}
