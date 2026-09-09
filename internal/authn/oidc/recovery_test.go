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

// TestTodo_AUTHN_002_Recovery is this todo's RECOVERY matrix test: a
// refused authorization attempt (expired, denied, or a failed exchange)
// never poisons the ability to authenticate again. A fresh
// BeginAuthorization after a failure mints an independent state/nonce/PKCE
// triple and succeeds normally, and the earlier failed attempt's state
// remains permanently unusable rather than being "recovered" for reuse.
func TestTodo_AUTHN_002_Recovery(t *testing.T) {
	t.Parallel()

	t.Run("retry_after_expired_pending_authorization", func(t *testing.T) {
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

		staleReq, staleCode := beginAndIssueCode(t, flow, idp, baseTime, "code-stale", "user-1", nil, keys, trustfederation.AlgRS256, kid)
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: staleReq.State, Code: staleCode, Now: baseTime.Add(20 * time.Minute)}); !errors.Is(err, oidc.ErrVerifierExpired) {
			t.Fatalf("stale attempt error = %v, want ErrVerifierExpired", err)
		}

		// A fresh attempt, later, on the same flow and issuer, must
		// succeed -- the earlier expiry is not a standing outage.
		freshReq, freshCode := beginAndIssueCode(t, flow, idp, baseTime.Add(21*time.Minute), "code-fresh", "user-1", nil, keys, trustfederation.AlgRS256, kid)
		if freshReq.State == staleReq.State {
			t.Fatalf("fresh attempt reused the stale attempt's state %q", staleReq.State)
		}
		principal, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: freshReq.State, Code: freshCode, Now: baseTime.Add(21*time.Minute + time.Second)})
		if err != nil {
			t.Fatalf("fresh HandleCallback: %v, want nil", err)
		}
		if principal.Subject() != "user-1" {
			t.Fatalf("fresh principal subject = %q, want user-1", principal.Subject())
		}

		// The stale attempt's state was already consumed by the first
		// HandleCallback call (Take is called before the TTL check), so it
		// stays refused rather than becoming retroactively valid.
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: staleReq.State, Code: staleCode, Now: baseTime.Add(22 * time.Minute)}); !errors.Is(err, oidc.ErrReplayedOrUnknownState) {
			t.Fatalf("re-replaying the stale state after recovery error = %v, want ErrReplayedOrUnknownState", err)
		}
	})

	t.Run("retry_after_denied_authorization", func(t *testing.T) {
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

		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{Error: "access_denied", Now: baseTime}); !errors.Is(err, oidc.ErrAuthorizationDenied) {
			t.Fatalf("denied callback error = %v, want ErrAuthorizationDenied", err)
		}

		req, code := beginAndIssueCode(t, flow, idp, baseTime.Add(time.Minute), "code-1", "user-1", nil, keys, trustfederation.AlgRS256, kid)
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: code, Now: baseTime.Add(time.Minute + time.Second)}); err != nil {
			t.Fatalf("retry after denial: %v, want nil", err)
		}
	})

	t.Run("retry_after_rejected_grant", func(t *testing.T) {
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

		// First attempt: never register a code with the fake IdP at all,
		// so the exchange is rejected outright (as if the user abandoned
		// the IdP's login page and the code was never actually issued).
		firstReq, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
		if err != nil {
			t.Fatalf("BeginAuthorization: %v", err)
		}
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: firstReq.State, Code: "never-issued-code", Now: baseTime.Add(time.Second)}); !errors.Is(err, oidc.ErrGrantRejected) {
			t.Fatalf("rejected-grant callback error = %v, want ErrGrantRejected", err)
		}

		// A completely fresh attempt still succeeds.
		req, code := beginAndIssueCode(t, flow, idp, baseTime.Add(time.Minute), "code-1", "user-1", nil, keys, trustfederation.AlgRS256, kid)
		if _, _, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{State: req.State, Code: code, Now: baseTime.Add(time.Minute + time.Second)}); err != nil {
			t.Fatalf("retry after rejected grant: %v, want nil", err)
		}
	})
}
