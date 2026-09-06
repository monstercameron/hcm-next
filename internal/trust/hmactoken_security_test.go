package trust

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var hmacTestNow = time.Unix(150, 0).UTC()

func hmacVerifierForTest(t *testing.T, mutate func(*HMACVerifierConfig)) *HMACVerifier {
	t.Helper()
	cfg := HMACVerifierConfig{Key: []byte("01234567890123456789012345678901"), Issuer: "issuer", Audience: "audience", Now: func() time.Time { return hmacTestNow }}
	if mutate != nil {
		mutate(&cfg)
	}
	v, err := NewHMACVerifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func validClaimsForTest() Claims {
	return Claims{Issuer: "issuer", Audience: "audience", Subject: "subject-1", SubjectKind: "human", Tenant: "tenant-a", OrganizationScopeID: "org-a", Roles: []string{"reader"}, Purposes: []string{"operations"}, AuthenticationMethod: "bearer_token", Assurance: "high", SessionRef: "session-1", IssuedAtUnix: 100, ExpiresAtUnix: 200}
}

func signedPayloadForTest(v *HMACVerifier, payload string) string {
	body := tokenEncoding.EncodeToString([]byte(payload))
	return tokenPrefix + "." + body + "." + tokenEncoding.EncodeToString(v.sign(body))
}

func TestNewHMACVerifier_ValidatesConfigAndCopiesKey(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*HMACVerifierConfig)
		want   error
	}{
		{"short key", func(c *HMACVerifierConfig) { c.Key = []byte("short") }, ErrVerifierKey},
		{"missing issuer", func(c *HMACVerifierConfig) { c.Issuer = "" }, ErrVerifierIssuer},
		{"missing audience", func(c *HMACVerifierConfig) { c.Audience = "" }, ErrVerifierAudience},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewHMACVerifier(func() HMACVerifierConfig {
				c := HMACVerifierConfig{Key: []byte("01234567890123456789012345678901"), Issuer: "issuer", Audience: "audience"}
				tc.mutate(&c)
				return c
			}())
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewHMACVerifier = %v, want %v", err, tc.want)
			}
		})
	}
	key := []byte("01234567890123456789012345678901")
	v, err := NewHMACVerifier(HMACVerifierConfig{Key: key, Issuer: "issuer", Audience: "audience", Now: func() time.Time { return hmacTestNow }, MaxTokenBytes: 17})
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'X'
	if reflect.DeepEqual(v.key, key) {
		t.Fatal("verifier retained caller-owned key storage")
	}
	if v.maxTokenBytes != 17 {
		t.Fatalf("max token bytes = %d", v.maxTokenBytes)
	}
	if got := hmacVerifierForTest(t, nil); got.maxTokenBytes != maxTokenBytesDefault {
		t.Fatalf("default max token bytes = %d", got.maxTokenBytes)
	}
}

func TestHMACVerifier_IssueVerifyAndCredentialBinding(t *testing.T) {
	v := hmacVerifierForTest(t, nil)
	token, err := v.Issue(validClaimsForTest())
	if err != nil || !strings.HasPrefix(token, tokenPrefix+".") {
		t.Fatalf("Issue = %q, %v", token, err)
	}
	p, err := v.Verify(context.Background(), Credential{Scheme: "bEaReR", Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject() != "subject-1" || p.Tenant() != "tenant-a" || p.Assurance() != AssuranceHigh || p.AuthenticationMethod() != AuthenticationMethodBearerToken || p.CredentialDigest() != credentialDigest(token) {
		t.Fatalf("verified principal = %s", p)
	}
	if _, err := v.Verify(context.Background(), Credential{Token: token, Audience: "wrong"}); !errors.Is(err, ErrWrongAudience) {
		t.Fatalf("audience override error = %v", err)
	}
	if _, err := v.Verify(context.Background(), Credential{Token: token, Audience: "audience"}); err != nil {
		t.Fatalf("matching audience override rejected: %v", err)
	}
	sum := sha256.Sum256([]byte(token))
	if want := "cred:sha256:" + hexForTest(sum[:]); p.CredentialDigest() != want {
		t.Fatalf("credential digest = %q, want %q", p.CredentialDigest(), want)
	}
}

func hexForTest(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2], out[i*2+1] = digits[v>>4], digits[v&15]
	}
	return string(out)
}

func TestHMACVerifier_RejectsForgedMalformedAndExpiredCredentials(t *testing.T) {
	v := hmacVerifierForTest(t, nil)
	validToken, err := v.Issue(validClaimsForTest())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		cred Credential
		want error
	}{
		{"missing token", Credential{}, ErrNoCredential},
		{"unsupported scheme", Credential{Scheme: "Basic", Token: validToken}, ErrUnsupportedScheme},
		{"wrong prefix", Credential{Token: "other." + validToken}, ErrInvalidCredential},
		{"missing signature", Credential{Token: tokenPrefix + ".body"}, ErrInvalidCredential},
		{"extra signature separator", Credential{Token: tokenPrefix + ".body.sig.extra"}, ErrInvalidCredential},
		{"invalid signature encoding", Credential{Token: tokenPrefix + ".body.!"}, ErrInvalidCredential},
		{"forged signature", Credential{Token: validToken[:len(validToken)-1] + "A"}, ErrInvalidCredential},
		{"invalid payload encoding", Credential{Token: signedPayloadForTest(v, "!")}, ErrInvalidCredential},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := v.Verify(context.Background(), tc.cred); !errors.Is(err, tc.want) {
				t.Fatalf("Verify = %v, want %v", err, tc.want)
			}
		})
	}
	short := hmacVerifierForTest(t, func(c *HMACVerifierConfig) { c.MaxTokenBytes = 5 })
	if _, err := short.Verify(context.Background(), Credential{Token: validToken}); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("oversized credential = %v", err)
	}
	for _, tc := range []struct {
		name   string
		claims Claims
		want   error
	}{
		{"unknown json field", func() Claims { return validClaimsForTest() }(), ErrInvalidCredential},
		{"wrong issuer", func() Claims { c := validClaimsForTest(); c.Issuer = "other"; return c }(), ErrWrongIssuer},
		{"missing validity", func() Claims { c := validClaimsForTest(); c.IssuedAtUnix = 0; return c }(), ErrExpiredCredential},
		{"expired", func() Claims { c := validClaimsForTest(); c.ExpiresAtUnix = 150; return c }(), ErrExpiredCredential},
		{"not yet valid", func() Claims { c := validClaimsForTest(); c.IssuedAtUnix = 151; return c }(), ErrExpiredCredential},
		{"missing assurance", func() Claims { c := validClaimsForTest(); c.Assurance = ""; return c }(), ErrMissingAssurance},
		{"bad subject kind", func() Claims { c := validClaimsForTest(); c.SubjectKind = "root"; return c }(), ErrInvalidCredential},
		{"bad authentication method", func() Claims { c := validClaimsForTest(); c.AuthenticationMethod = "password"; return c }(), ErrInvalidCredential},
		{"invalid principal tenant", func() Claims { c := validClaimsForTest(); c.Tenant = "bad tenant"; return c }(), ErrInvalidCredential},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := "{}"
			if tc.name == "unknown json field" {
				payload = `{"iss":"issuer","aud":"audience","sub":"subject-1","sub_kind":"human","tenant":"tenant-a","authn_method":"bearer_token","assurance":"high","session_ref":"session-1","iat":100,"exp":200,"unknown":true}`
			} else {
				body, marshalErr := marshalClaimsForTest(tc.claims)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				payload = body
			}
			if _, err := v.Verify(context.Background(), Credential{Token: signedPayloadForTest(v, payload)}); !errors.Is(err, tc.want) {
				t.Fatalf("Verify = %v, want %v", err, tc.want)
			}
		})
	}
}

func marshalClaimsForTest(c Claims) (string, error) {
	data, err := jsonMarshalForTest(c)
	return string(data), err
}

func jsonMarshalForTest(v any) ([]byte, error) {
	// Keep this helper local so all malformed-token cases still go through Issue's
	// canonical JSON field spellings except where the test intentionally bypasses it.
	return json.Marshal(v)
}

func TestHMACVerifier_RejectsTrailingClaimsAndAcceptsAllWireEnums(t *testing.T) {
	v := hmacVerifierForTest(t, nil)
	base := validClaimsForTest()
	payload, err := marshalClaimsForTest(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), Credential{Token: signedPayloadForTest(v, payload+` {"extra":true}`)}); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("trailing JSON accepted: %v", err)
	}
	for _, assurance := range []string{"low", "substantial", "high"} {
		for _, kind := range []string{"human", "service", "agent", "integration"} {
			for _, method := range []string{"bearer_token", "mutual_tls"} {
				claims := base
				claims.Assurance, claims.SubjectKind, claims.AuthenticationMethod = assurance, kind, method
				body, marshalErr := marshalClaimsForTest(claims)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				if _, verifyErr := v.Verify(context.Background(), Credential{Token: signedPayloadForTest(v, body)}); verifyErr != nil {
					t.Fatalf("valid enum combination %s/%s/%s rejected: %v", assurance, kind, method, verifyErr)
				}
			}
		}
	}
}
