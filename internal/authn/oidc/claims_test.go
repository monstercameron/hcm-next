package oidc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/authn/oidc"
	"github.com/monstercameron/hcm-next/internal/trust"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

// mappedClaimScenario builds an issuer with mappings, signs an ID token
// carrying extraClaims plus the standard set, completes a callback, and
// returns the resulting principal (or error). This exercises claims.go's
// unexported mapClaims/applyClaimMapping through the one public entry point
// that calls them.
func mappedClaimScenario(t *testing.T, mappings []issuerregistry.ClaimMapping, extraClaims map[string]any) (*trust.Principal, error) {
	t.Helper()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		mappings, 0,
	)
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)

	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	challenge := queryValue(t, req.URL, "code_challenge")
	nonce := queryValue(t, req.URL, "nonce")
	claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
	for k, v := range extraClaims {
		claims[k] = v
	}
	idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
	idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: "at-1"})

	principal, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
	return principal, err
}

func TestMapClaims_AllPrincipalFields(t *testing.T) {
	t.Parallel()
	mappings := []issuerregistry.ClaimMapping{
		{SourceClaim: "platform_sub", Target: issuerregistry.PrincipalFieldSubject},
		{SourceClaim: "kind", Target: issuerregistry.PrincipalFieldSubjectKind},
		{SourceClaim: "org_scope", Target: issuerregistry.PrincipalFieldOrganizationScopeID},
		{SourceClaim: "roles", Target: issuerregistry.PrincipalFieldRoles},
		{SourceClaim: "authority_refs", Target: issuerregistry.PrincipalFieldAuthorityRefs},
		{SourceClaim: "purposes", Target: issuerregistry.PrincipalFieldPurposes},
		{SourceClaim: "assurance", Target: issuerregistry.PrincipalFieldAssurance},
		{SourceClaim: "delegation_refs", Target: issuerregistry.PrincipalFieldDelegationRefs},
	}
	principal, err := mappedClaimScenario(t, mappings, map[string]any{
		"platform_sub":    "overridden-subject",
		"kind":            "service",
		"org_scope":       "org-emea",
		"roles":           []string{"role-a", "role-b"},
		"authority_refs":  []string{"authority:x"},
		"purposes":        "purpose_a purpose_b", // space-delimited form
		"assurance":       "high",
		"delegation_refs": []string{"delegation:1"},
	})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if principal.Subject() != "overridden-subject" {
		t.Fatalf("Subject = %q, want overridden-subject", principal.Subject())
	}
	if principal.SubjectKind() != trust.SubjectKindService {
		t.Fatalf("SubjectKind = %v, want service", principal.SubjectKind())
	}
	if principal.OrganizationScopeID() != "org-emea" {
		t.Fatalf("OrganizationScopeID = %q, want org-emea", principal.OrganizationScopeID())
	}
	if !principal.HasRole("role-a") || !principal.HasRole("role-b") {
		t.Fatalf("Roles = %v, want role-a and role-b", principal.Roles())
	}
	if want, got := []string{"authority:x"}, principal.AuthorityRefs(); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("AuthorityRefs = %v, want %v", got, want)
	}
	if !principal.AuthorizesPurpose("purpose_a") || !principal.AuthorizesPurpose("purpose_b") {
		t.Fatalf("Purposes = %v, want purpose_a and purpose_b (space-delimited claim split)", principal.Purposes())
	}
	if principal.Assurance() != trust.AssuranceHigh {
		t.Fatalf("Assurance = %v, want high", principal.Assurance())
	}
	if want, got := []string{"delegation:1"}, principal.DelegationRefs(); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("DelegationRefs = %v, want %v", got, want)
	}
}

func TestMapClaims_UnmappedClaimNeverLeaksAuthority(t *testing.T) {
	t.Parallel()
	// No claim mappings at all: even though the token carries a "roles"
	// claim, it must never reach the principal -- governance metadata
	// (the issuer's own ClaimMappings), not the raw token, decides what is
	// trusted.
	principal, err := mappedClaimScenario(t, nil, map[string]any{"roles": []string{"should-not-appear"}})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if len(principal.Roles()) != 0 {
		t.Fatalf("Roles = %v, want none (roles claim was never mapped)", principal.Roles())
	}
}

func TestMapClaims_RefusesUnknownEnumValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		mappings []issuerregistry.ClaimMapping
		claims   map[string]any
	}{
		{
			"unknown sub_kind",
			[]issuerregistry.ClaimMapping{{SourceClaim: "kind", Target: issuerregistry.PrincipalFieldSubjectKind}},
			map[string]any{"kind": "not-a-real-kind"},
		},
		{
			"unknown assurance",
			[]issuerregistry.ClaimMapping{{SourceClaim: "lvl", Target: issuerregistry.PrincipalFieldAssurance}},
			map[string]any{"lvl": "not-a-real-level"},
		},
		{
			"wrong JSON type for a string field",
			[]issuerregistry.ClaimMapping{{SourceClaim: "org_scope", Target: issuerregistry.PrincipalFieldOrganizationScopeID}},
			map[string]any{"org_scope": 12345},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mappedClaimScenario(t, tc.mappings, tc.claims)
			if !errors.Is(err, oidc.ErrClaimMapping) {
				t.Fatalf("HandleCallback error = %v, want ErrClaimMapping", err)
			}
		})
	}
}
