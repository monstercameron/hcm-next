package oidc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/authn/oidc"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

// TestTodo_AUTHN_002_Integration drives the flow with every seam this
// package ships a production adapter for actually engaged: a real
// net/http.Server standing in for the identity provider's token endpoint
// (exercised through [oidc.HTTPTokenExchanger], not a hand-rolled fake),
// signing keys resolved through [issuerregistry.NewTenantResolver] exactly
// as a composition root would wire it, and the full HTTP round trip a
// browser redirect would drive (parsing the authorization URL's query
// string the same way a callback handler receives it).
func TestTodo_AUTHN_002_Integration(t *testing.T) {
	keys := newTestKeys(t)
	kid := "rsa-integration-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		[]issuerregistry.ClaimMapping{
			{SourceClaim: "roles", Target: issuerregistry.PrincipalFieldRoles},
		},
		time.Minute,
	)

	// tokenSrv stands in for the identity provider's real token endpoint:
	// it parses the exact wire form oidc.HTTPTokenExchanger sends and
	// answers with a real HTTP response, over a real TCP connection.
	var lastForm url.Values
	var nonceForToken string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		lastForm = r.PostForm
		if r.PostForm.Get("code") != "integration-code-1" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "unknown code"})
			return
		}
		claims := idTokenClaims(fixture.issuer.IssuerURL, audienceAcme, "integration-user", nonceForToken, baseTime)
		claims["roles"] = []string{"comp_admin"}
		idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "integration-access-token",
			"token_type":   "Bearer",
			"id_token":     idToken,
		})
	}))
	defer tokenSrv.Close()

	clients := oidc.NewStaticClientSource().WithClient(oidc.ClientRegistration{
		Tenant: tenantAcme, IssuerURL: issuerAcme,
		ClientID: clientIDAcme, RedirectURI: redirectURI,
		AuthorizationEndpoint: authEndpoint,
		TokenEndpoint:         tokenSrv.URL,
		Scopes:                []string{"openid"},
	})
	flow, err := oidc.NewFlow(oidc.FlowConfig{
		Registry:  fixture.store,
		Keys:      fixture.resolver,
		Clients:   clients,
		States:    oidc.NewMemoryStateStore(),
		Exchanger: oidc.HTTPTokenExchanger{},
		Secret:    testFlowSecret,
	})
	if err != nil {
		t.Fatalf("NewFlow: %v", err)
	}

	req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	// A real browser callback delivers "state" and "code" as query
	// parameters; parse the authorization URL the same way a handler
	// would extract the nonce it needs to hand the fake token endpoint (in
	// production the identity provider itself carries the nonce forward
	// into the ID token; here the test server needs it to mint one).
	authURL, err := url.Parse(req.URL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	nonceForToken = authURL.Query().Get("nonce")

	principal, ev, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{
		State: req.State,
		Code:  "integration-code-1",
		Now:   baseTime.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if principal.Subject() != "integration-user" {
		t.Fatalf("principal subject = %q, want integration-user", principal.Subject())
	}
	if !principal.HasRole("comp_admin") {
		t.Fatalf("principal roles = %v, want comp_admin", principal.Roles())
	}
	if ev.Outcome != oidc.OutcomeSuccess {
		t.Fatalf("evidence outcome = %q, want SUCCESS", ev.Outcome)
	}
	if lastForm.Get("grant_type") != "authorization_code" {
		t.Fatalf("token endpoint saw grant_type = %q, want authorization_code", lastForm.Get("grant_type"))
	}
	if lastForm.Get("redirect_uri") != redirectURI {
		t.Fatalf("token endpoint saw redirect_uri = %q, want %q", lastForm.Get("redirect_uri"), redirectURI)
	}
	if lastForm.Get("code_verifier") == "" {
		t.Fatalf("token endpoint saw no code_verifier")
	}
}
