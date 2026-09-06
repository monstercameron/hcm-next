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

// beginAndIssueCode drives BeginAuthorization, extracts the code_challenge
// the request carries, registers a matching code with idp, and returns the
// AuthorizationRequest plus the code so a test can call HandleCallback.
func beginAndIssueCode(t *testing.T, flow *oidc.Flow, idp *fakeIdP, now time.Time, code, subject string, extraClaims map[string]any, keys *testKeys, alg trustfederation.Algorithm, kid string) (oidc.AuthorizationRequest, string) {
	t.Helper()
	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, now)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	challenge := queryValue(t, req.URL, "code_challenge")
	nonce := queryValue(t, req.URL, "nonce")

	claims := idTokenClaims(issuerAcme, audienceAcme, subject, nonce, now)
	for k, v := range extraClaims {
		claims[k] = v
	}
	idToken := keys.sign(t, alg, kid, claims)

	idp.issueCode(code, fakeCode{
		codeChallenge: challenge,
		redirectURI:   redirectURI,
		clientID:      clientIDAcme,
		idToken:       idToken,
		accessToken:   "at-" + code,
	})
	return req, code
}

// TestTodo_AUTHN_002 is this todo's PRIMARY test: a complete
// authorization-code-with-PKCE round trip -- BeginAuthorization mints
// state/nonce/PKCE material bound to an ACTIVE issuer, HandleCallback
// consumes it exactly once, exchanges the code through the injected
// [oidc.TokenExchanger], validates the returned ID token against the
// issuer's pinned material, and maps its claims onto a [trust.Principal].
func TestTodo_AUTHN_002(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		[]issuerregistry.ClaimMapping{
			{SourceClaim: "roles", Target: issuerregistry.PrincipalFieldRoles},
			{SourceClaim: "org_scope", Target: issuerregistry.PrincipalFieldOrganizationScopeID},
			{SourceClaim: "assurance", Target: issuerregistry.PrincipalFieldAssurance},
		},
		time.Minute,
	)
	idp := newFakeIdP()
	sink := &recordingSink{}
	flow := newFlow(t, fixture, newClientSource(), idp, sink)

	req, code := beginAndIssueCode(t, flow, idp, baseTime, "code-1", "user-42",
		map[string]any{
			"roles":     []string{"comp_admin", "approver"},
			"org_scope": "org-north-america",
			"assurance": "substantial",
		}, keys, trustfederation.AlgRS256, kid)

	principal, ev, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{
		State: req.State, Code: code, Now: baseTime.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if principal == nil {
		t.Fatalf("HandleCallback returned a nil principal on success")
	}
	if principal.Tenant() != tenantAcme {
		t.Fatalf("principal tenant = %q, want %q", principal.Tenant(), tenantAcme)
	}
	if principal.Subject() != "user-42" {
		t.Fatalf("principal subject = %q, want user-42", principal.Subject())
	}
	if !principal.HasRole("comp_admin") || !principal.HasRole("approver") {
		t.Fatalf("principal roles = %v, want comp_admin and approver", principal.Roles())
	}
	if principal.OrganizationScopeID() != "org-north-america" {
		t.Fatalf("principal org scope = %q, want org-north-america", principal.OrganizationScopeID())
	}
	if principal.Assurance() != trust.AssuranceSubstantial {
		t.Fatalf("principal assurance = %v, want substantial", principal.Assurance())
	}
	if principal.AuthenticationMethod() != trust.AuthenticationMethodBearerToken {
		t.Fatalf("principal authentication method = %v, want bearer token", principal.AuthenticationMethod())
	}
	if principal.SubjectKind() != trust.SubjectKindHuman {
		t.Fatalf("principal subject kind = %v, want human (the default for an interactive browser flow)", principal.SubjectKind())
	}
	if ev.Outcome != oidc.OutcomeSuccess {
		t.Fatalf("evidence outcome = %q, want SUCCESS", ev.Outcome)
	}
	if ev.EvidenceID == "" {
		t.Fatalf("evidence has no EvidenceID")
	}
	if ev.Subject != "user-42" {
		t.Fatalf("evidence subject = %q, want user-42", ev.Subject)
	}

	records := sink.all()
	if len(records) != 1 || records[0].EvidenceID != ev.EvidenceID {
		t.Fatalf("recording sink = %+v, want exactly the returned evidence", records)
	}

	// The same (state, code) pair can never be redeemed twice.
	if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{
		State: req.State, Code: code, Now: baseTime.Add(2 * time.Second),
	}); !errors.Is(err, oidc.ErrReplayedOrUnknownState) {
		t.Fatalf("replayed callback error = %v, want ErrReplayedOrUnknownState", err)
	}
}

func TestFlow_HandleCallback_SubjectDefaultsToVerifiedSub(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "ec-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgES256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgES256, kid)},
		nil, // no claim mappings at all
		0,
	)
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)

	req, code := beginAndIssueCode(t, flow, idp, baseTime, "code-1", "user-plain", nil, keys, trustfederation.AlgES256, kid)

	principal, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: code, Now: baseTime.Add(time.Second)})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if principal.Subject() != "user-plain" {
		t.Fatalf("principal subject = %q, want user-plain (the token's own sub claim)", principal.Subject())
	}
	if principal.Assurance() != trust.AssuranceLow {
		t.Fatalf("principal assurance = %v, want low (safe default with no assurance claim mapped)", principal.Assurance())
	}
	if len(principal.Roles()) != 0 {
		t.Fatalf("principal roles = %v, want none (no roles claim mapped)", principal.Roles())
	}
}

func TestFlow_HandleCallback_EdDSA(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "ed-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgEdDSA},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgEdDSA, kid)},
		nil, 0,
	)
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)

	req, code := beginAndIssueCode(t, flow, idp, baseTime, "code-1", "user-ed", nil, keys, trustfederation.AlgEdDSA, kid)
	principal, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: code, Now: baseTime.Add(time.Second)})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if principal.Subject() != "user-ed" {
		t.Fatalf("principal subject = %q, want user-ed", principal.Subject())
	}
}

func TestFlow_HandleCallback_AtHashMatch(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, 0,
	)
	idp := newFakeIdP()
	flow := newFlow(t, fixture, newClientSource(), idp, nil)

	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	challenge := queryValue(t, req.URL, "code_challenge")
	nonce := queryValue(t, req.URL, "nonce")
	accessToken := "access-token-value"
	claims := idTokenClaims(issuerAcme, audienceAcme, "user-athash", nonce, baseTime)
	claims["at_hash"] = computeAtHashForTest(accessToken)
	idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
	idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: accessToken})

	principal, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if principal.Subject() != "user-athash" {
		t.Fatalf("principal subject = %q, want user-athash", principal.Subject())
	}
}

func TestNewFlow_ValidatesConfig(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, "rsa-1")},
		nil, 0,
	)
	base := oidc.FlowConfig{
		Registry:  fixture.store,
		Keys:      fixture.resolver,
		Clients:   newClientSource(),
		States:    oidc.NewMemoryStateStore(),
		Exchanger: newFakeIdP(),
	}

	cases := []struct {
		name    string
		mutate  func(oidc.FlowConfig) oidc.FlowConfig
		wantErr bool
	}{
		{"valid", func(c oidc.FlowConfig) oidc.FlowConfig { return c }, false},
		{"nil registry", func(c oidc.FlowConfig) oidc.FlowConfig { c.Registry = nil; return c }, true},
		{"nil keys", func(c oidc.FlowConfig) oidc.FlowConfig { c.Keys = nil; return c }, true},
		{"nil clients", func(c oidc.FlowConfig) oidc.FlowConfig { c.Clients = nil; return c }, true},
		{"nil states", func(c oidc.FlowConfig) oidc.FlowConfig { c.States = nil; return c }, true},
		{"nil exchanger", func(c oidc.FlowConfig) oidc.FlowConfig { c.Exchanger = nil; return c }, true},
		{"short secret", func(c oidc.FlowConfig) oidc.FlowConfig { c.Secret = []byte("too-short"); return c }, true},
		{"32 byte secret ok", func(c oidc.FlowConfig) oidc.FlowConfig { c.Secret = make([]byte, 32); return c }, false},
		{"nil secret mints one", func(c oidc.FlowConfig) oidc.FlowConfig { c.Secret = nil; return c }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := oidc.NewFlow(tc.mutate(base))
			if tc.wantErr && err == nil {
				t.Fatalf("NewFlow: got nil error, want one")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("NewFlow: %v, want nil", err)
			}
		})
	}
}

func TestFlow_BeginAuthorization_RequiresNow(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, "rsa-1")},
		nil, 0,
	)
	flow := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil)
	if _, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, time.Time{}); err == nil {
		t.Fatalf("BeginAuthorization with zero now: got nil error, want one")
	}
}
