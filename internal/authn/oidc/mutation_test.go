package oidc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/authn/oidc"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

// TestTodo_AUTHN_002_Mutation is this todo's MUTATION matrix test: it pins
// down exact boundary values a mutation of a comparison operator (< vs <=,
// == vs !=, > vs >=) would flip without any other test in this package
// noticing. Each subtest asserts both sides of one boundary.
func TestTodo_AUTHN_002_Mutation(t *testing.T) {
	t.Parallel()

	t.Run("pending_authorization_ttl_boundary", func(t *testing.T) {
		t.Parallel()
		keys := newTestKeys(t)
		kid := "rsa-1"
		fixture := newIssuerFixture(t,
			[]trustfederation.Algorithm{trustfederation.AlgRS256},
			[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
			nil, 0,
		)

		// Exactly at the TTL boundary (10 minutes, the default): still
		// valid. A mutant that turned "!After" into "!Before" (or added an
		// off-by-one) would flip one side of this pair.
		idp1 := newFakeIdP()
		flow1 := newFlow(t, fixture, newClientSource(), idp1, nil)
		req1, code1 := beginAndIssueCode(t, flow1, idp1, baseTime, "code-1", "user-1", nil, keys, trustfederation.AlgRS256, kid)
		if _, _, err := flow1.HandleCallback(context.Background(), oidc.CallbackParams{State: req1.State, Code: code1, Now: baseTime.Add(10 * time.Minute)}); err != nil {
			t.Fatalf("HandleCallback exactly at TTL boundary: %v, want nil", err)
		}

		// One nanosecond past the boundary: refused.
		idp2 := newFakeIdP()
		flow2 := newFlow(t, fixture, newClientSource(), idp2, nil)
		req2, code2 := beginAndIssueCode(t, flow2, idp2, baseTime, "code-2", "user-1", nil, keys, trustfederation.AlgRS256, kid)
		if _, _, err := flow2.HandleCallback(context.Background(), oidc.CallbackParams{State: req2.State, Code: code2, Now: baseTime.Add(10*time.Minute + time.Nanosecond)}); !errors.Is(err, oidc.ErrVerifierExpired) {
			t.Fatalf("HandleCallback 1ns past TTL boundary error = %v, want ErrVerifierExpired", err)
		}
	})

	t.Run("id_token_expiry_boundary_with_skew", func(t *testing.T) {
		t.Parallel()
		keys := newTestKeys(t)
		kid := "rsa-1"
		skew := 30 * time.Second
		fixture := newIssuerFixture(t,
			[]trustfederation.Algorithm{trustfederation.AlgRS256},
			[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
			nil, skew,
		)

		// now is exactly exp+skew: the check is "now.Before(exp+skew)", so
		// this must already be refused (strictly before, not
		// before-or-equal). A mutant weakening Before to !After would
		// accept this. idTokenScenario's fixed Now=baseTime+1s does not
		// land on this exact boundary, so this case is built directly
		// below instead of through that shared helper.
		idp := newFakeIdP()
		flow := newFlow(t, fixture, newClientSource(), idp, nil)
		req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
		if err != nil {
			t.Fatalf("BeginAuthorization: %v", err)
		}
		challenge := queryValue(t, req.URL, "code_challenge")
		nonce := queryValue(t, req.URL, "nonce")
		exp := baseTime.Add(9 * time.Minute)
		claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
		claims["exp"] = exp.Unix()
		idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
		idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: "at-1"})

		// now == exp + skew exactly: refused (strict "before").
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: exp.Add(skew)}); !errors.Is(err, oidc.ErrIDTokenExpired) {
			t.Fatalf("HandleCallback at now==exp+skew error = %v, want ErrIDTokenExpired", err)
		}
	})

	t.Run("id_token_expiry_one_tick_before_boundary", func(t *testing.T) {
		t.Parallel()
		keys := newTestKeys(t)
		kid := "rsa-1"
		skew := 30 * time.Second
		fixture := newIssuerFixture(t,
			[]trustfederation.Algorithm{trustfederation.AlgRS256},
			[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
			nil, skew,
		)
		idp := newFakeIdP()
		flow := newFlow(t, fixture, newClientSource(), idp, nil)
		req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
		if err != nil {
			t.Fatalf("BeginAuthorization: %v", err)
		}
		challenge := queryValue(t, req.URL, "code_challenge")
		nonce := queryValue(t, req.URL, "nonce")
		exp := baseTime.Add(9 * time.Minute)
		claims := idTokenClaims(issuerAcme, audienceAcme, "user-1", nonce, baseTime)
		claims["exp"] = exp.Unix()
		idToken := keys.sign(t, trustfederation.AlgRS256, kid, claims)
		idp.issueCode("code-1", fakeCode{codeChallenge: challenge, redirectURI: redirectURI, clientID: clientIDAcme, idToken: idToken, accessToken: "at-1"})

		// One nanosecond before now==exp+skew: still valid.
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: "code-1", Now: exp.Add(skew).Add(-time.Nanosecond)}); err != nil {
			t.Fatalf("HandleCallback 1ns before now==exp+skew: %v, want nil", err)
		}
	})

	t.Run("multi_audience_threshold", func(t *testing.T) {
		t.Parallel()
		keys := newTestKeys(t)
		kid := "rsa-1"
		fixture := newIssuerFixture(t,
			[]trustfederation.Algorithm{trustfederation.AlgRS256},
			[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
			nil, 0,
		)
		// Exactly one audience (no azp required): valid. A mutant that
		// turned "len > 1" into "len >= 1" would require azp even here.
		if err := idTokenScenario(t, fixture, keys, trustfederation.AlgRS256, kid, func(c map[string]any) {
			c["aud"] = []string{audienceAcme}
		}); err != nil {
			t.Fatalf("single-element aud array: %v, want nil", err)
		}
		// Exactly two audiences without azp: refused.
		if err := idTokenScenario(t, fixture, keys, trustfederation.AlgRS256, kid, func(c map[string]any) {
			c["aud"] = []string{audienceAcme, "second-audience"}
		}); !errors.Is(err, oidc.ErrWrongAudience) {
			t.Fatalf("two-element aud array without azp error = %v, want ErrWrongAudience", err)
		}
	})
}
