package oidc_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/authn/oidc"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// FuzzTodo_AUTHN_002 fuzzes [oidc.Flow.HandleCallback]'s callback and ID
// token surface with adversarial byte strings. The property under test is
// purely "never panics, never returns a principal without also returning a
// nil error" -- every malformed or hostile input must be classified as one
// of this package's typed refusals, never crash the process it runs in.
func FuzzTodo_AUTHN_002(f *testing.F) {
	f.Add("", "", "")
	f.Add("state-1", "code-1", "")
	f.Add("state-1", "code-1", "not.a.jwt")
	f.Add("state-1", "code-1", "..")
	f.Add("state-1", "code-1", "eyJhbGciOiJub25lIn0.e30.")
	f.Add("state-1", "code-1", "AAAA.BBBB.CCCC")
	f.Add("'; DROP TABLE issuers; --", "code-1", "")
	f.Add("state-1", "code-1", string([]byte{0x00, 0xff, 0xfe, 'a', 'b'}))

	kid := "rsa-fuzz-1"

	f.Fuzz(func(t *testing.T, state, code, forgedIDToken string) {
		// Built per-iteration from *testing.T rather than *testing.F: that
		// type deliberately does not implement testing.TB (this package's
		// fixture helpers require it), matching the same constraint
		// internal/trust/federation's own fuzz test works around.
		keys := newTestKeys(t)
		fixture := newIssuerFixture(t,
			[]trustfederation.Algorithm{trustfederation.AlgRS256},
			[]issuerregistry.PinnedKey{keys.pinnedKey(t, trustfederation.AlgRS256, kid)},
			nil, time.Minute,
		)
		idp := newFakeIdP()
		flow := newFlow(t, fixture, newClientSource(), idp, nil)

		// Give the fuzzer a real, legitimately issued pending
		// authorization to aim at some of the time (otherwise almost every
		// input is rejected at the very first "unknown state" check and
		// the ID-token parser underneath is never reached).
		req, err := flow.BeginAuthorization(context.Background(), tenantAcme, issuerAcme, baseTime)
		if err != nil {
			t.Fatalf("BeginAuthorization: %v", err)
		}
		challenge := queryValue(t, req.URL, "code_challenge")
		idp.issueCode(req.State, oidcFuzzCode(challenge, forgedIDToken))

		useState, useCode := state, code
		if state == "" {
			useState = req.State
		}
		if code == "" {
			useCode = req.State // deliberately wrong on purpose sometimes; any string is fine
		}

		principal, ev, err := flow.HandleCallback(context.Background(), oidc.CallbackParams{
			State: useState, Code: useCode, Now: baseTime.Add(time.Second),
		})
		if err == nil && principal == nil {
			t.Fatalf("HandleCallback: nil error but nil principal")
		}
		if err != nil && principal != nil {
			t.Fatalf("HandleCallback: non-nil error %v but non-nil principal", err)
		}
		if ev.EvidenceID == "" {
			t.Fatalf("HandleCallback: empty EvidenceID")
		}
	})
}

// oidcFuzzCode builds a [fakeCode] whose id_token is exactly the fuzzer's
// candidate string, bound to the real challenge the corpus entry's
// authorization request actually used, so a corpus entry that supplies the
// legitimate state/code pair still exercises ID-token parsing with hostile
// bytes.
func oidcFuzzCode(challenge, forgedIDToken string) fakeCode {
	return fakeCode{
		codeChallenge: challenge,
		redirectURI:   redirectURI,
		clientID:      clientIDAcme,
		idToken:       forgedIDToken,
		accessToken:   "at-fuzz",
	}
}
