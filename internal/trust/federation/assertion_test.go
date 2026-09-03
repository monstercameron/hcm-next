package federation_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/federation"
)

func newValidator(t *testing.T, keys federation.KeySource, at time.Time) *federation.Validator {
	t.Helper()
	v, err := federation.NewValidator(federation.Config{
		TenantIssuers: map[values.TenantId][]string{
			tenantAcme:  {issuerAcme},
			tenantOther: {issuerOther},
		},
		Audience: audienceUnder,
		Keys:     keys,
		Now:      func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return v
}

// TestTodo_TRUST_002 is the TRUST-002 primary test.
//
// GREEN: a signature-verified enterprise assertion whose issuer is
// allow-listed for the tenant it claims, whose audience matches this
// listener and whose validity window covers the evaluation instant yields
// the same immutable trust.Principal a first-party HMAC credential would,
// through the frozen trust.NewPrincipal constructor.
//
// RED: an assertion is rejected when it fails PKCE-adjacent identity checks
// this package owns: an issuer mix-up (a genuinely signed assertion for one
// tenant re-labeled with another tenant's claim), a wrong audience, a stale
// signing key, a substituted signature, or a missing required claim. Each is
// a separate subtest so a regression names itself.
func TestTodo_TRUST_002(t *testing.T) {
	keys := newTestKeys(t)
	v := newValidator(t, keys.source, baseTime)
	ctx := context.Background()

	t.Run("RS256 assertion yields the full principal", func(t *testing.T) {
		claims := validAcmeClaims()
		token := keys.signRS256(t, claims, "acme-rsa-1")
		principal, err := v.Validate(ctx, token)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if got := principal.Tenant().String(); got != claims.Tenant {
			t.Errorf("tenant = %q, want %q", got, claims.Tenant)
		}
		if got := principal.Subject(); got != claims.Subject {
			t.Errorf("subject = %q, want %q", got, claims.Subject)
		}
		if got := principal.SubjectKind(); got != trust.SubjectKindHuman {
			t.Errorf("subject kind = %v, want human", got)
		}
		if got := principal.AuthenticationMethod(); got != trust.AuthenticationMethodBearerToken {
			t.Errorf("authentication method = %v, want bearer_token", got)
		}
		if got := principal.Assurance(); got != trust.AssuranceSubstantial {
			t.Errorf("assurance = %v, want substantial", got)
		}
		if !principal.HasRole("intent_author") {
			t.Error("principal should hold the intent_author role")
		}
		if principal.SessionRef() == "" {
			t.Error("principal must carry a session reference")
		}
		if !strings.HasPrefix(principal.EvidenceID(), "ev:authn:") {
			t.Errorf("evidence id = %q, want an ev:authn: reference", principal.EvidenceID())
		}
		if !strings.HasPrefix(principal.CredentialDigest(), "cred:sha256:") {
			t.Errorf("credential digest = %q, want a cred:sha256: digest", principal.CredentialDigest())
		}
	})

	t.Run("ES256 and EdDSA assertions authenticate through the same path", func(t *testing.T) {
		esToken := keys.signES256(t, validAcmeClaims(), "acme-ec-1")
		if _, err := v.Validate(ctx, esToken); err != nil {
			t.Errorf("Validate(ES256): %v", err)
		}
		otherValidator := newValidator(t, keys.source, baseTime)
		edToken := keys.signEdDSA(t, validOtherClaims(), "other-ed-1")
		principal, err := otherValidator.Validate(ctx, edToken)
		if err != nil {
			t.Fatalf("Validate(EdDSA): %v", err)
		}
		if got := principal.Tenant().String(); got != string(tenantOther) {
			t.Errorf("tenant = %q, want %q", got, tenantOther)
		}
	})

	t.Run("RED: issuer mix-up across tenants is rejected", func(t *testing.T) {
		// The assertion is genuinely signed by acme's own key, but claims to
		// be for tenant-other -- acme's issuer is not allow-listed there.
		claims := validAcmeClaims()
		claims.Tenant = string(tenantOther)
		token := keys.signRS256(t, claims, "acme-rsa-1")
		_, err := v.Validate(ctx, token)
		if !errors.Is(err, federation.ErrTenantBinding) {
			t.Fatalf("Validate(issuer mix-up) = %v, want ErrTenantBinding", err)
		}
	})

	t.Run("RED: an issuer no tenant recognizes is rejected", func(t *testing.T) {
		claims := validAcmeClaims()
		claims.Issuer = "https://attacker.example/"
		// Sign with acme's key but claim a foreign issuer: KeySource has no
		// entry for the foreign issuer, so key resolution itself fails
		// before the mismatched issuer claim would even matter.
		token := keys.signRS256(t, claims, "acme-rsa-1")
		_, err := v.Validate(ctx, token)
		if err == nil {
			t.Fatal("Validate(unknown issuer) succeeded, want a failure")
		}
	})

	t.Run("RED: wrong audience is rejected", func(t *testing.T) {
		claims := validAcmeClaims()
		claims.Audience = "some-other-listener"
		token := keys.signRS256(t, claims, "acme-rsa-1")
		if _, err := v.Validate(ctx, token); !errors.Is(err, federation.ErrAssertionAudience) {
			t.Fatalf("Validate(wrong audience) = %v, want ErrAssertionAudience", err)
		}
	})

	t.Run("RED: expired assertion is rejected", func(t *testing.T) {
		claims := validAcmeClaims()
		token := keys.signRS256(t, claims, "acme-rsa-1")
		expired := newValidator(t, keys.source, time.Unix(claims.ExpiresAtUnix, 0).Add(time.Second))
		if _, err := expired.Validate(ctx, token); !errors.Is(err, federation.ErrAssertionExpired) {
			t.Fatalf("Validate after expiry = %v, want ErrAssertionExpired", err)
		}
	})

	t.Run("RED: not-yet-valid (nbf) assertion is rejected, skew tolerated within leeway", func(t *testing.T) {
		claims := validAcmeClaims()
		claims.NotBeforeUnix = baseTime.Add(time.Minute).Unix()
		token := keys.signRS256(t, claims, "acme-rsa-1")

		if _, err := v.Validate(ctx, token); !errors.Is(err, federation.ErrAssertionExpired) {
			t.Fatalf("Validate before nbf = %v, want ErrAssertionExpired", err)
		}

		skewed, err := federation.NewValidator(federation.Config{
			TenantIssuers: map[values.TenantId][]string{tenantAcme: {issuerAcme}},
			Audience:      audienceUnder,
			Keys:          keys.source,
			Now:           func() time.Time { return baseTime },
			Leeway:        2 * time.Minute,
		})
		if err != nil {
			t.Fatalf("NewValidator: %v", err)
		}
		if _, err := skewed.Validate(ctx, token); err != nil {
			t.Fatalf("Validate within leeway = %v, want success", err)
		}
	})

	t.Run("RED: stale signing key is rejected", func(t *testing.T) {
		staleSource := federation.NewStaticKeySource().WithKey(issuerAcme, federation.SigningKey{
			ID:        "acme-rsa-1",
			Algorithm: federation.AlgRS256,
			Public:    &keys.rsaKey.PublicKey,
			NotAfter:  baseTime.Add(-time.Hour),
		})
		staleValidator := newValidator(t, staleSource, baseTime)
		token := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
		if _, err := staleValidator.Validate(ctx, token); !errors.Is(err, federation.ErrKeyExpired) {
			t.Fatalf("Validate(stale key) = %v, want ErrKeyExpired", err)
		}
	})

	t.Run("RED: substituted signature (token substitution) is rejected", func(t *testing.T) {
		tokenA := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
		tokenB := keys.signES256(t, validAcmeClaims(), "acme-ec-1")
		partsA := strings.Split(tokenA, ".")
		partsB := strings.Split(tokenB, ".")
		substituted := partsA[0] + "." + partsA[1] + "." + partsB[2]
		if _, err := v.Validate(ctx, substituted); err == nil {
			t.Fatal("Validate(substituted signature) succeeded, want a failure")
		}
	})

	t.Run("RED: missing required claims are rejected", func(t *testing.T) {
		for name, mutate := range map[string]func(*federation.Claims){
			"no subject":      func(c *federation.Claims) { c.Subject = "" },
			"no tenant":       func(c *federation.Claims) { c.Tenant = "" },
			"no subject kind": func(c *federation.Claims) { c.SubjectKind = "" },
			"no assurance":    func(c *federation.Claims) { c.Assurance = "" },
			"no issuer":       func(c *federation.Claims) { c.Issuer = "" },
		} {
			claims := validAcmeClaims()
			mutate(&claims)
			token := keys.signRS256(t, claims, "acme-rsa-1")
			if _, err := v.Validate(ctx, token); err == nil {
				t.Errorf("Validate(%s) succeeded, want a failure", name)
			}
		}
	})

	t.Run("context helpers carry exactly what was validated", func(t *testing.T) {
		token := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
		principal, err := v.Validate(ctx, token)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		withPrincipal := trust.WithPrincipal(ctx, principal)
		got, ok := trust.FromContext(withPrincipal)
		if !ok || got.Fingerprint() != principal.Fingerprint() {
			t.Fatal("FromContext did not return the validated principal")
		}
	})
}
