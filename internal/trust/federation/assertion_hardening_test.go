package federation_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

func TestNewValidator_RejectsIncompleteConfigurationAndCopiesInputs(t *testing.T) {
	keys := newTestKeys(t)
	validIssuers := map[values.TenantId][]string{tenantAcme: {issuerAcme}}
	tests := []struct {
		name string
		cfg  federation.Config
		want error
	}{
		{name: "missing audience", cfg: federation.Config{Keys: keys.source, TenantIssuers: validIssuers}, want: federation.ErrValidatorAudience},
		{name: "missing keys", cfg: federation.Config{Audience: audienceUnder, TenantIssuers: validIssuers}, want: federation.ErrValidatorKeys},
		{name: "missing issuers", cfg: federation.Config{Audience: audienceUnder, Keys: keys.source}, want: federation.ErrValidatorIssuers},
		{name: "invalid tenant", cfg: federation.Config{Audience: audienceUnder, Keys: keys.source, TenantIssuers: map[values.TenantId][]string{"!bad": {issuerAcme}}}, want: federation.ErrValidatorIssuers},
		{name: "empty issuer list", cfg: federation.Config{Audience: audienceUnder, Keys: keys.source, TenantIssuers: map[values.TenantId][]string{tenantAcme: {}}}, want: federation.ErrValidatorIssuers},
		{name: "empty issuer", cfg: federation.Config{Audience: audienceUnder, Keys: keys.source, TenantIssuers: map[values.TenantId][]string{tenantAcme: {""}}}, want: federation.ErrValidatorIssuers},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := federation.NewValidator(tt.cfg); !errors.Is(err, tt.want) {
				t.Fatalf("NewValidator error = %v, want errors.Is(..., %v)", err, tt.want)
			}
		})
	}

	issuers := map[values.TenantId][]string{tenantAcme: {issuerAcme}}
	v, err := federation.NewValidator(federation.Config{TenantIssuers: issuers, Audience: audienceUnder, Keys: keys.source, Now: func() time.Time { return baseTime }, MaxAssertionBytes: -1})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	issuers[tenantAcme][0] = "https://mutated.invalid/"
	issuers[tenantAcme] = append(issuers[tenantAcme], "https://added.invalid/")
	if _, err := v.Validate(context.Background(), keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")); err != nil {
		t.Fatalf("validator changed after caller mutation: %v", err)
	}
}

func assertionWithHeaderAndPayload(header, payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".AAAA"
}

func TestValidator_RejectsHeaderAndClaimParserBoundariesBeforeKeyUse(t *testing.T) {
	keys := newTestKeys(t)
	v := newValidator(t, keys.source, baseTime)
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "unexpected typ", raw: assertionWithHeaderAndPayload("{\"alg\":\"RS256\",\"kid\":\"acme-rsa-1\",\"typ\":\"JWS\"}", "{}"), want: federation.ErrMalformedAssertion},
		{name: "unsupported algorithm", raw: assertionWithHeaderAndPayload("{\"alg\":\"none\",\"kid\":\"acme-rsa-1\"}", "{}"), want: federation.ErrUnsupportedAlgorithm},
		{name: "missing key id", raw: assertionWithHeaderAndPayload("{\"alg\":\"RS256\"}", "{}"), want: federation.ErrMalformedAssertion},
		{name: "trailing header value", raw: assertionWithHeaderAndPayload("{\"alg\":\"RS256\",\"kid\":\"acme-rsa-1\"} {}", "{}"), want: federation.ErrMalformedAssertion},
		{name: "trailing claims value", raw: assertionWithHeaderAndPayload("{\"alg\":\"RS256\",\"kid\":\"acme-rsa-1\"}", "{\"iss\":\"issuer\"} {}"), want: federation.ErrMalformedAssertion},
		{name: "malformed claims json", raw: assertionWithHeaderAndPayload("{\"alg\":\"RS256\",\"kid\":\"acme-rsa-1\"}", "{"), want: federation.ErrMalformedAssertion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := v.Validate(context.Background(), tt.raw); !errors.Is(err, tt.want) {
				t.Fatalf("Validate = %v, want errors.Is(..., %v)", err, tt.want)
			}
		})
	}
}

func TestValidator_RejectsClaimAndKeyPolicyBranchesWithSentinels(t *testing.T) {
	keys := newTestKeys(t)
	v := newValidator(t, keys.source, baseTime)
	tests := []struct {
		name   string
		mutate func(*federation.Claims)
		want   error
	}{
		{name: "missing issuer", mutate: func(c *federation.Claims) { c.Issuer = "" }, want: federation.ErrAssertionClaims},
		{name: "invalid tenant", mutate: func(c *federation.Claims) { c.Tenant = "!invalid" }, want: federation.ErrAssertionClaims},
		{name: "unknown issuer", mutate: func(c *federation.Claims) { c.Issuer = "https://unknown.invalid/" }, want: federation.ErrAssertionIssuer},
		{name: "missing subject", mutate: func(c *federation.Claims) { c.Subject = "" }, want: federation.ErrAssertionClaims},
		{name: "missing validity", mutate: func(c *federation.Claims) { c.IssuedAtUnix = 0 }, want: federation.ErrAssertionExpired},
		{name: "exp before iat", mutate: func(c *federation.Claims) { c.ExpiresAtUnix = c.IssuedAtUnix }, want: federation.ErrAssertionExpired},
		{name: "invalid assurance", mutate: func(c *federation.Claims) { c.Assurance = "unknown" }, want: federation.ErrAssertionAssurance},
		{name: "invalid subject kind", mutate: func(c *federation.Claims) { c.SubjectKind = "unknown" }, want: federation.ErrAssertionClaims},
		{name: "wrong audience", mutate: func(c *federation.Claims) { c.Audience = "other" }, want: federation.ErrAssertionAudience},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := validAcmeClaims()
			tt.mutate(&claims)
			if _, err := v.Validate(context.Background(), keys.signRS256(t, claims, "acme-rsa-1")); !errors.Is(err, tt.want) {
				t.Fatalf("Validate = %v, want errors.Is(..., %v)", err, tt.want)
			}
		})
	}

	claims := validAcmeClaims()
	claims.Issuer = issuerOther
	claims.Tenant = string(tenantAcme)
	if _, err := v.Validate(context.Background(), keys.signRS256(t, claims, "acme-rsa-1")); !errors.Is(err, federation.ErrTenantBinding) {
		t.Fatalf("issuer known for another tenant = %v, want ErrTenantBinding", err)
	}
	claims = validAcmeClaims()
	if _, err := v.Validate(context.Background(), keys.signRS256(t, claims, "missing-kid")); !errors.Is(err, federation.ErrKeyNotFound) {
		t.Fatalf("missing resolved key = %v, want ErrKeyNotFound", err)
	}

	stale := federation.NewStaticKeySource().WithKey(issuerAcme, federation.SigningKey{ID: "acme-rsa-1", Algorithm: federation.AlgRS256, Public: &keys.rsaKey.PublicKey, NotAfter: baseTime})
	if _, err := newValidator(t, stale, baseTime).Validate(context.Background(), keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")); !errors.Is(err, federation.ErrKeyExpired) {
		t.Fatalf("expired key = %v, want ErrKeyExpired", err)
	}
}

func TestValidator_MapsAllSupportedWireEnumsAndRedactsClaims(t *testing.T) {
	keys := newTestKeys(t)
	v := newValidator(t, keys.source, baseTime)
	for _, assurance := range []string{"low", "substantial", "high"} {
		for _, kind := range []string{"human", "service", "agent", "integration"} {
			claims := validAcmeClaims()
			claims.Assurance = assurance
			claims.SubjectKind = kind
			if _, err := v.Validate(context.Background(), keys.signRS256(t, claims, "acme-rsa-1")); err != nil {
				t.Fatalf("assurance=%q subject_kind=%q: %v", assurance, kind, err)
			}
		}
	}
	claims := validAcmeClaims()
	claims.Subject = "subject-secret"
	token := keys.signRS256(t, claims, "acme-rsa-1")
	principal, err := v.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	rendered := principal.String() + "|" + principal.CredentialDigest() + "|" + principal.EvidenceID() + "|" + principal.SessionRef()
	if strings.Contains(rendered, token) || strings.Contains(rendered, strings.SplitN(token, ".", 3)[1]) {
		t.Fatalf("principal rendered raw claim/assertion: %q", rendered)
	}
}
