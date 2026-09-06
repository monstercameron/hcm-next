package oidc_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

// TestFlow_BeginAuthorization_PKCEParameters exercises pkce.go's
// randomToken/deriveVerifier/codeChallengeS256 (all unexported) through the
// only public entry point that calls them: [oidc.Flow.BeginAuthorization].
// It asserts the shape RFC 7636 requires (S256 method, a 43-character
// base64url challenge) and that two independent authorization attempts
// never share a state, nonce or challenge.
func TestFlow_BeginAuthorization_PKCEParameters(t *testing.T) {
	t.Parallel()
	keys := newTestKeys(t)
	kid := "rsa-1"
	fixture := newIssuerFixture(t,
		[]trustfederation.Algorithm{trustfederation.AlgRS256},
		[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
		nil, 0,
	)
	flow := newFlow(t, fixture, newClientSource(), newFakeIdP(), nil)

	first, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("first BeginAuthorization: %v", err)
	}
	second, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
	if err != nil {
		t.Fatalf("second BeginAuthorization: %v", err)
	}

	if first.State == "" || first.Nonce == "" {
		t.Fatalf("first request has empty state/nonce: %+v", first)
	}
	if first.State == second.State {
		t.Fatalf("two BeginAuthorization calls produced the same state %q", first.State)
	}
	if first.Nonce == second.Nonce {
		t.Fatalf("two BeginAuthorization calls produced the same nonce %q", first.Nonce)
	}

	method := queryValue(t, first.URL, "code_challenge_method")
	if method != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", method)
	}
	challenge1 := queryValue(t, first.URL, "code_challenge")
	challenge2 := queryValue(t, second.URL, "code_challenge")
	if len(challenge1) != 43 {
		t.Fatalf("code_challenge length = %d, want 43 (base64url of a 32-byte SHA-256 digest)", len(challenge1))
	}
	if challenge1 == challenge2 {
		t.Fatalf("two BeginAuthorization calls (different states) produced the same code_challenge")
	}
	if queryValue(t, first.URL, "state") != first.State {
		t.Fatalf("authorization URL state query param does not match AuthorizationRequest.State")
	}
	if queryValue(t, first.URL, "nonce") != first.Nonce {
		t.Fatalf("authorization URL nonce query param does not match AuthorizationRequest.Nonce")
	}
	if queryValue(t, first.URL, "redirect_uri") != redirectURI {
		t.Fatalf("authorization URL redirect_uri = %q, want the registered %q", queryValue(t, first.URL, "redirect_uri"), redirectURI)
	}
	if queryValue(t, first.URL, "response_type") != "code" {
		t.Fatalf("authorization URL response_type = %q, want code", queryValue(t, first.URL, "response_type"))
	}
}
