package oidc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// TestTodo_AUTHN_002_Security is this todo's SECURITY matrix test. AUTHN-002's
// own RED clause names "state/nonce/PKCE replay, redirect confusion and
// token substitution"; the lane instructions additionally name "replay,
// downgrade, wrong-issuer and PKCE-mismatch". Every one of those six cases
// is its own subtest below.
func TestTodo_AUTHN_002_Security(t *testing.T) {
	t.Parallel()

	t.Run("replayed_state_and_code", func(t *testing.T) {
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
		req, code := beginAndIssueCode(t, flow, idp, baseTime, "code-1", "user-1", nil, keys, trustfederation.AlgRS256, kid)

		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: code, Now: baseTime.Add(time.Second)}); err != nil {
			t.Fatalf("first HandleCallback: %v, want nil", err)
		}
		// Replaying the identical callback -- same state, same code -- a
		// second time must never succeed, whether the attacker captured it
		// off the wire or it is simply delivered twice (a doubled browser
		// request, a retried webhook).
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: code, Now: baseTime.Add(2 * time.Second)}); !errors.Is(err, oidc.ErrReplayedOrUnknownState) {
			t.Fatalf("replayed callback error = %v, want ErrReplayedOrUnknownState", err)
		}
	})

	t.Run("downgraded_algorithm", func(t *testing.T) {
		t.Parallel()
		keys := newTestKeys(t)
		// Issuer declares only ES256; the identity provider (or an
		// attacker in the middle) hands back a token whose header claims
		// RS256. Even though RS256 is in this package's closed vocabulary
		// and keys.rsaKey is a syntactically valid RSA key, the issuer's
		// own governed algorithm set never named RS256, so this must be
		// refused before any key is even resolved.
		fixture := newIssuerFixture(t,
			[]trustfederation.Algorithm{trustfederation.AlgES256},
			[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgES256, "ec-1")},
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
		claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
		forged := keys.sign(t, trustfederation.AlgRS256, "rsa-not-pinned", claims)
		idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: forged, accessToken: "at-1"})

		_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
		if !errors.Is(err, oidc.ErrUnsupportedAlgorithm) {
			t.Fatalf("downgraded-algorithm callback error = %v, want ErrUnsupportedAlgorithm", err)
		}
	})

	t.Run("wrong_issuer", func(t *testing.T) {
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
		// The token verifies (it is genuinely signed by this issuer's own
		// pinned key) but its iss claim names a different issuer -- an
		// issuer mix-up / confused-deputy attempt this flow must catch
		// independent of signature validity.
		claims := idTokenClaims("https://a-different-issuer.invalid/", audienceAcme, "user-1", nonce, baseTime)
		forged := keys.sign(t, trustfederation.AlgRS256, kid, claims)
		idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: forged, accessToken: "at-1"})

		_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
		if !errors.Is(err, oidc.ErrWrongIssuer) {
			t.Fatalf("wrong-issuer callback error = %v, want ErrWrongIssuer", err)
		}
	})

	t.Run("pkce_mismatch", func(t *testing.T) {
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
		nonce := queryValue(t, req.URL, "nonce")
		claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
		idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
		// The fake IdP performs the same RFC 7636 verification a real
		// token endpoint does: the code is bound to a code_challenge that
		// does NOT match what this flow actually requested (simulating a
		// code obtained under a different, attacker-controlled
		// authorization request being replayed against this flow's
		// callback).
		idp.issueCode("code-1", fakeCode{codeChallenge: "wrong-challenge-not-what-was-requested", redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: "at-1"})

		_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
		if !errors.Is(err, oidc.ErrGrantRejected) {
			t.Fatalf("PKCE-mismatch callback error = %v, want ErrGrantRejected", err)
		}
	})

	t.Run("redirect_confusion", func(t *testing.T) {
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
		claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
		idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
		// The IdP-recorded redirect_uri for this code is an
		// attacker-controlled URI, not the tenant's registered
		// redirectURI. Because [oidc.Flow.HandleCallback] always sends
		// the registered [oidc.ClientRegistration.RedirectURI] -- never a
		// value from the request -- the token exchange itself must fail
		// this binding check.
		idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: "https://attacker.invalid/callback", clientID: clientIDAcme, idToken: idToken, accessToken: "at-1"})

		_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
		if !errors.Is(err, oidc.ErrGrantRejected) {
			t.Fatalf("redirect-confusion callback error = %v, want ErrGrantRejected", err)
		}
	})

	t.Run("access_token_substitution", func(t *testing.T) {
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
		claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
		// The ID token's at_hash is bound to "at-legitimate", but the
		// token response substitutes a different access token -- exactly
		// the token-substitution attack at_hash exists to catch.
		claims["at_hash"] = computeAtHashForTest("at-legitimate")
		idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
		idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: "at-substituted"})

		_, _, err = flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: baseTime.Add(time.Second)})
		if !errors.Is(err, oidc.ErrAccessTokenMismatch) {
			t.Fatalf("access-token-substitution callback error = %v, want ErrAccessTokenMismatch", err)
		}
	})
}
