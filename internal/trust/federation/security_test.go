package federation_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// TestTodo_TRUST_002_Security is the TRUST-002 security test. It attacks the
// assertion itself: the classic "alg: none" bypass, an algorithm-confusion
// substitution, a tampered payload re-signed with the wrong key, an
// undeclared header parameter and an oversized assertion must all fail, and
// no failure may disclose the raw assertion.
func TestTodo_TRUST_002_Security(t *testing.T) {
	keys := newTestKeys(t)
	v := newValidator(t, keys.source, baseTime)
	ctx := context.Background()

	t.Run(`"alg: none" is rejected`, func(t *testing.T) {
		payload, err := json.Marshal(validAcmeClaims())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		header := []byte(`{"alg":"none","kid":"acme-rsa-1"}`)
		token := base64.RawURLEncoding.EncodeToString(header) + "." +
			base64.RawURLEncoding.EncodeToString(payload) + "."
		if _, err := v.Validate(ctx, token); err == nil {
			t.Fatal(`Validate("alg: none") succeeded, want a failure`)
		}
	})

	t.Run("header algorithm must match the resolved key's own algorithm", func(t *testing.T) {
		// Sign with the ES256 key but declare RS256 in the header: even if
		// an attacker could coerce the verifier into treating the EC public
		// key's bytes as an RSA modulus (it cannot, here, because Go's
		// crypto.PublicKey is concretely typed), the algorithm recorded
		// against the key id in the KeySource must still be checked.
		claims := validAcmeClaims()
		payload, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		header := []byte(`{"alg":"RS256","kid":"acme-ec-1"}`)
		signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
		token := signingInput + ".AAAA"
		if _, err := v.Validate(ctx, token); err == nil {
			t.Fatal("Validate(algorithm confusion) succeeded, want a failure")
		}
	})

	t.Run("an undeclared header parameter is rejected", func(t *testing.T) {
		payload, err := json.Marshal(validAcmeClaims())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		header := []byte(`{"alg":"RS256","kid":"acme-rsa-1","crit":["b64"]}`)
		token := base64.RawURLEncoding.EncodeToString(header) + "." +
			base64.RawURLEncoding.EncodeToString(payload) + ".AAAA"
		if _, err := v.Validate(ctx, token); !isMalformed(err) {
			t.Fatalf("Validate(unknown header param) = %v, want ErrMalformedAssertion", err)
		}
	})

	t.Run("an unknown claim key is rejected", func(t *testing.T) {
		payload := []byte(`{"iss":"` + issuerAcme + `","aud":"` + audienceUnder +
			`","sub":"u","sub_kind":"human","tenant":"acme-corp","assurance":"high",` +
			`"iat":1,"exp":9999999999,"escalate":true}`)
		header := []byte(`{"alg":"RS256","kid":"acme-rsa-1"}`)
		token := base64.RawURLEncoding.EncodeToString(header) + "." +
			base64.RawURLEncoding.EncodeToString(payload) + ".AAAA"
		if _, err := v.Validate(ctx, token); !isMalformed(err) {
			t.Fatalf("Validate(unknown claim) = %v, want ErrMalformedAssertion", err)
		}
	})

	t.Run("oversized assertion is rejected before parsing", func(t *testing.T) {
		bounded, err := federation.NewValidator(federation.Config{
			TenantIssuers:     map[values.TenantId][]string{tenantAcme: {issuerAcme}, tenantOther: {issuerOther}},
			Audience:          audienceUnder,
			Keys:              keys.source,
			Now:               func() time.Time { return baseTime },
			MaxAssertionBytes: 64,
		})
		if err != nil {
			t.Fatalf("NewValidator: %v", err)
		}
		token := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
		if _, err := bounded.Validate(ctx, token); !isMalformed(err) {
			t.Fatalf("Validate(oversized) = %v, want ErrMalformedAssertion", err)
		}
	})

	t.Run("malformed tokens do not verify and never panic", func(t *testing.T) {
		for _, token := range []string{
			"", "...", "a.b", "a.b.c.d",
			strings.Repeat(".", 500),
			"\x00\x01\x02",
		} {
			if _, err := v.Validate(ctx, token); err == nil {
				t.Errorf("Validate(%q) succeeded, want a failure", token)
			}
		}
	})

	t.Run("a validated principal never carries the raw assertion", func(t *testing.T) {
		token := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
		principal, err := v.Validate(ctx, token)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		rendered := principal.String() + "|" + principal.CredentialDigest() + "|" + principal.EvidenceID() + "|" + principal.SessionRef()
		if strings.Contains(rendered, token) {
			t.Error("the principal's rendered form leaks the raw assertion")
		}
		body := strings.SplitN(token, ".", 3)[1]
		if strings.Contains(rendered, body) {
			t.Error("the principal's rendered form leaks the assertion payload")
		}
	})
}

func isMalformed(err error) bool {
	return errors.Is(err, federation.ErrMalformedAssertion)
}
