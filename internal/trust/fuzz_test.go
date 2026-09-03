package trust_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
)

// hmacSHA256 signs data with key. It reimplements the token signature so the
// fuzz target can produce correctly signed garbage as well as unsigned
// garbage: an implementation that only ever sees invalid signatures is never
// asked the interesting question.
func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// FuzzTodo_TRUST_001 is the TRUST-001 fuzz target.
//
// The invariant under test is one sentence: for any input whatsoever, Verify
// either returns an error and no principal, or returns a fully valid principal
// whose tenant, kind, method, assurance, session and validity window all pass
// their own rules. There is no third outcome, and in particular there is no
// partially populated principal.
func FuzzTodo_TRUST_001(f *testing.F) {
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      fixtureKey,
		Issuer:   fixtureIssuer,
		Audience: fixtureAudience,
		Now:      func() time.Time { return baseTime },
	})
	if err != nil {
		f.Fatalf("NewHMACVerifier: %v", err)
	}

	good, err := verifier.Issue(validClaims())
	if err != nil {
		f.Fatalf("Issue: %v", err)
	}

	f.Add("Bearer", good, false)
	f.Add("", good, false)
	f.Add("Bearer", "", false)
	f.Add("Basic", good, false)
	f.Add("Bearer", `{"iss":"x"}`, true)
	f.Add("Bearer", `{"iss":"`+fixtureIssuer+`","aud":"`+fixtureAudience+`","sub":"u","sub_kind":"human","tenant":"acme-corp","authn_method":"bearer_token","assurance":"high","session_ref":"s","iat":1,"exp":9999999999}`, true)
	f.Add("Bearer", strings.Repeat("A", 4096), false)
	f.Add("bearer", "hcmn1..", false)
	f.Add("Bearer", "hcmn1.\x00\x01.\xff", false)

	ctx := context.Background()

	f.Fuzz(func(t *testing.T, scheme, payload string, sign bool) {
		token := payload
		if sign {
			// Present the payload as a correctly signed token, so the fuzzer
			// exercises the claim rules rather than stopping at the signature.
			body := base64.RawURLEncoding.EncodeToString([]byte(payload))
			token = "hcmn1." + body + "." + base64.RawURLEncoding.EncodeToString(hmacSHA256(fixtureKey, "hcmn1."+body))
		}

		principal, err := verifier.Verify(ctx, trust.Credential{Scheme: scheme, Token: token})
		if err != nil {
			if principal != nil {
				t.Fatalf("Verify returned both an error (%v) and a principal", err)
			}
			return
		}
		if principal == nil {
			t.Fatal("Verify returned neither an error nor a principal")
		}

		// A returned principal is fully formed. Nothing here is a restatement
		// of the parser: these are the invariants the rest of the platform
		// relies on without re-checking.
		if err := principal.Tenant().Validate(); err != nil {
			t.Fatalf("verified principal has an invalid tenant %q: %v", principal.Tenant(), err)
		}
		if principal.Subject() == "" {
			t.Fatal("verified principal has an empty subject")
		}
		if principal.SubjectKind() == trust.SubjectKindUnspecified {
			t.Fatal("verified principal has an unspecified subject kind")
		}
		if principal.AuthenticationMethod() == trust.AuthenticationMethodUnspecified {
			t.Fatal("verified principal has an unspecified authentication method")
		}
		if principal.Assurance() == trust.AssuranceUnspecified {
			t.Fatal("verified principal has no assurance evidence")
		}
		if principal.SessionRef() == "" {
			t.Fatal("verified principal has no session reference")
		}
		if !principal.ExpiresAt().After(principal.IssuedAt()) {
			t.Fatalf("verified principal has an empty validity window [%s, %s)", principal.IssuedAt(), principal.ExpiresAt())
		}
		if principal.IssuedAt().After(baseTime) || !baseTime.Before(principal.ExpiresAt()) {
			t.Fatalf("verified principal is not valid at the verification instant %s", baseTime)
		}
		if !strings.HasPrefix(principal.EvidenceID(), "ev:authn:") {
			t.Fatalf("verified principal has evidence id %q", principal.EvidenceID())
		}
		if len(principal.Fingerprint()) != 64 {
			t.Fatalf("fingerprint %q is not a sha256 hex digest", principal.Fingerprint())
		}
		if strings.Contains(principal.String(), token) && token != "" {
			t.Fatal("the principal's rendered form leaks the credential")
		}
	})
}
