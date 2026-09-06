package workload

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	identityTestIssuer = "identity-test-issuer"
	identityTestCell   = "cell-test"
)

var identityTestNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func identityAuthority(t *testing.T) (*Issuer, *StaticKeySource, ed25519.PrivateKey) {
	t.Helper()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewIssuer(IssuerConfig{Name: identityTestIssuer, KeyID: "key-1", Private: private, Now: func() time.Time { return identityTestNow }})
	if err != nil {
		t.Fatal(err)
	}
	return issuer, NewStaticKeySource().WithKey(identityTestIssuer, "key-1", pub), private
}

func identityVerifier(t *testing.T, keys KeySource, now time.Time, leeway time.Duration) *Verifier {
	t.Helper()
	v, err := NewVerifier(VerifierConfig{Keys: keys, Cell: identityTestCell, Now: func() time.Time { return now }, Leeway: leeway})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func signedIdentityToken(t *testing.T, private ed25519.PrivateKey, c claims, suffix string) string {
	t.Helper()
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return signedIdentityPayload(private, payload, suffix)
}

func signedIdentityPayload(private ed25519.PrivateKey, payload []byte, suffix string) string {
	body := tokenEncoding.EncodeToString(payload)
	input := tokenPrefix + "." + body
	sig := ed25519.Sign(private, []byte(input))
	return input + "." + tokenEncoding.EncodeToString(sig) + suffix
}

func validIdentityClaims() claims {
	return claims{Issuer: identityTestIssuer, Subject: "worker-1", Role: string(RoleWorker), Cell: identityTestCell, KeyID: "key-1", IssuedAtUnix: identityTestNow.Unix(), ExpiresAtUnix: identityTestNow.Add(5 * time.Minute).Unix()}
}

func TestIdentity_PublicSurfaceAndValidityBoundaries(t *testing.T) {
	issuer, source, _ := identityAuthority(t)
	token, err := issuer.Issue(IssueSpec{Subject: "worker-1", Role: RoleWorker, Cell: identityTestCell, Lifetime: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	id, err := identityVerifier(t, source, identityTestNow, 0).Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if id.Issuer() != identityTestIssuer || id.Subject() != "worker-1" || id.Role() != RoleWorker || id.Cell() != identityTestCell || id.KeyID() != "key-1" {
		t.Fatalf("identity accessors lost trusted fields: %+v", id)
	}
	if !id.IssuedAt().Equal(identityTestNow) || !id.ExpiresAt().Equal(identityTestNow.Add(5*time.Minute)) || id.Fingerprint() == "" {
		t.Fatalf("identity time/fingerprint accessors incorrect: issued=%s expires=%s fingerprint=%q", id.IssuedAt(), id.ExpiresAt(), id.Fingerprint())
	}
	if !id.ValidAt(id.IssuedAt()) || id.ValidAt(id.ExpiresAt()) || id.ValidAt(id.IssuedAt().Add(-time.Nanosecond)) {
		t.Fatal("identity validity boundaries are not [issued, expires)")
	}
	if rendered := id.String(); rendered == "" || strings.Contains(rendered, token) || !strings.Contains(rendered, "workload(") {
		t.Fatalf("String() is not redacted/log-safe: %q", rendered)
	}
	for _, role := range []ProcessRole{RoleHCMNext, RoleWorker, RoleProjector, RoleMigrate, RoleScheduler, RoleAdmin} {
		if !role.Valid() {
			t.Errorf("declared role %q was not valid", role)
		}
	}
	if ProcessRole("forged").Valid() {
		t.Fatal("undeclared role was accepted")
	}
}

func TestIdentity_ConstructorsAndIssueValidation(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewIssuer(IssuerConfig{}); !errors.Is(err, ErrIssuerName) {
		t.Fatalf("NewIssuer(empty) = %v, want ErrIssuerName", err)
	}
	if _, err := NewIssuer(IssuerConfig{Name: "issuer"}); !errors.Is(err, ErrIssuerKey) {
		t.Fatalf("NewIssuer(missing key) = %v, want ErrIssuerKey", err)
	}
	if _, err := NewIssuer(IssuerConfig{Name: "issuer", KeyID: "kid", Private: private[:ed25519.PrivateKeySize-1]}); !errors.Is(err, ErrIssuerKey) {
		t.Fatalf("NewIssuer(short key) = %v, want ErrIssuerKey", err)
	}
	issuer, err := NewIssuer(IssuerConfig{Name: "issuer", KeyID: "kid", Private: private, Now: func() time.Time { return identityTestNow }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVerifier(VerifierConfig{}); !errors.Is(err, ErrVerifierKeys) {
		t.Fatalf("NewVerifier(no keys) = %v, want ErrVerifierKeys", err)
	}
	if _, err := NewVerifier(VerifierConfig{Keys: NewStaticKeySource()}); !errors.Is(err, ErrVerifierCell) {
		t.Fatalf("NewVerifier(no cell) = %v, want ErrVerifierCell", err)
	}
	cases := []struct {
		name string
		spec IssueSpec
		want error
	}{
		{"subject", IssueSpec{Role: RoleWorker, Cell: identityTestCell, Lifetime: time.Minute}, ErrInvalidIssueSpec},
		{"role", IssueSpec{Subject: "worker", Role: "forged", Cell: identityTestCell, Lifetime: time.Minute}, ErrInvalidIssueSpec},
		{"cell", IssueSpec{Subject: "worker", Role: RoleWorker, Lifetime: time.Minute}, ErrInvalidIssueSpec},
		{"zero lifetime", IssueSpec{Subject: "worker", Role: RoleWorker, Cell: identityTestCell}, ErrInvalidIssueSpec},
		{"long lifetime", IssueSpec{Subject: "worker", Role: RoleWorker, Cell: identityTestCell, Lifetime: MaxIdentityLifetime + time.Nanosecond}, ErrLifetimeTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := issuer.Issue(tc.spec); !errors.Is(err, tc.want) {
				t.Fatalf("Issue(%s) = %v, want %v", tc.name, err, tc.want)
			}
		})
	}
	if token, err := issuer.Issue(IssueSpec{Subject: "worker", Role: RoleWorker, Cell: identityTestCell, Lifetime: MaxIdentityLifetime}); err != nil || token == "" {
		t.Fatalf("Issue(exact maximum) = %q, %v; want a credential", token, err)
	}
	if _, err := NewStaticKeySource().ResolveKey(context.Background(), "missing", "kid"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("ResolveKey(missing issuer) = %v, want ErrKeyNotFound", err)
	}
	if _, err := NewStaticKeySource().WithKey("issuer", "kid", pub).ResolveKey(context.Background(), "issuer", "missing"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("ResolveKey(missing key) = %v, want ErrKeyNotFound", err)
	}
	keySource := NewStaticKeySource().WithKey("issuer", "kid", pub)
	got, err := keySource.ResolveKey(context.Background(), "issuer", "kid")
	if err != nil || !got.Equal(pub) {
		t.Fatalf("ResolveKey(valid) = %v, %v", got, err)
	}
}

func TestIdentity_VerifyRejectsMalformedAndForgedCredentials(t *testing.T) {
	issuer, source, private := identityAuthority(t)
	v := identityVerifier(t, source, identityTestNow, 0)
	valid, err := issuer.Issue(IssueSpec{Subject: "worker-1", Role: RoleWorker, Cell: identityTestCell, Lifetime: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	validClaims := validIdentityClaims()
	validParts := strings.Split(valid, ".")
	badSignature := validParts[0] + "." + validParts[1] + "." + tokenEncoding.EncodeToString([]byte("short"))
	malformed := []struct {
		name  string
		token string
		want  error
	}{
		{"empty", "", ErrMalformedCredential},
		{"prefix", "wlid2.a.b", ErrMalformedCredential},
		{"missing parts", "wlid1..", ErrMalformedCredential},
		{"signature encoding", "wlid1." + tokenEncoding.EncodeToString([]byte("{}")) + ".$", ErrMalformedCredential},
		{"payload encoding", "wlid1.*.abc", ErrMalformedCredential},
		{"bad signature length", badSignature, ErrInvalidSignature},
	}
	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tc.token)
			if err == nil {
				t.Fatal("Verify unexpectedly succeeded")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Verify = %v, want %v", err, tc.want)
			}
		})
	}

	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"unknown issuer", signedIdentityToken(t, private, func() claims { c := validClaims; c.Issuer = "foreign"; return c }(), ""), ErrKeyNotFound},
		{"unknown key", signedIdentityToken(t, private, func() claims { c := validClaims; c.KeyID = "key-unknown"; return c }(), ""), ErrKeyNotFound},
		{"bad signature", validParts[0] + "." + validParts[1] + "." + tokenEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)), ErrInvalidSignature},
		{"unknown role", signedIdentityToken(t, private, func() claims { c := validClaims; c.Role = "forged"; return c }(), ""), ErrUnknownRole},
		{"wrong cell", signedIdentityToken(t, private, func() claims { c := validClaims; c.Cell = "cell-other"; return c }(), ""), ErrWrongCell},
		{"missing issuer", signedIdentityToken(t, private, func() claims { c := validClaims; c.Issuer = ""; return c }(), ""), ErrMalformedCredential},
		{"zero validity", signedIdentityToken(t, private, func() claims { c := validClaims; c.IssuedAtUnix = 0; return c }(), ""), ErrCredentialExpired},
		{"reversed validity", signedIdentityToken(t, private, func() claims { c := validClaims; c.ExpiresAtUnix = c.IssuedAtUnix; return c }(), ""), ErrCredentialExpired},
		{"too long", signedIdentityToken(t, private, func() claims {
			c := validClaims
			c.ExpiresAtUnix = c.IssuedAtUnix + int64(MaxIdentityLifetime/time.Second) + 1
			return c
		}(), ""), ErrLifetimeTooLong},
		{"trailing json", signedIdentityPayload(private, []byte(`{"iss":"identity-test-issuer","sub":"worker-1","role":"worker","cell":"cell-test","kid":"key-1","iat":1788696000,"exp":1788696300} {"extra":true}`), ""), ErrMalformedCredential},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tc.token)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Verify = %v, want %v", err, tc.want)
			}
		})
	}
	unknownClaim := `{"iss":"identity-test-issuer","sub":"worker-1","role":"worker","cell":"cell-test","kid":"key-1","iat":1788696000,"exp":1788696300,"extra":true}`
	if _, err := v.Verify(context.Background(), signedIdentityPayload(private, []byte(unknownClaim), "")); !errors.Is(err, ErrMalformedCredential) {
		t.Fatalf("unknown claim Verify = %v, want ErrMalformedCredential", err)
	}
	if _, err := v.Verify(context.Background(), valid+".extra"); !errors.Is(err, ErrMalformedCredential) {
		t.Fatalf("extra token part Verify = %v, want ErrMalformedCredential", err)
	}
}

func TestIdentity_VerifyTimeAndLeewayBranches(t *testing.T) {
	_, source, private := identityAuthority(t)
	base := validIdentityClaims()
	cases := []struct {
		name   string
		now    time.Time
		c      claims
		leeway time.Duration
		want   error
	}{
		{"expired", identityTestNow.Add(6 * time.Minute), base, 0, ErrCredentialExpired},
		{"not yet", identityTestNow.Add(-time.Minute), base, 0, ErrCredentialExpired},
		{"leeway accepts expiry skew", identityTestNow.Add(5*time.Minute + 30*time.Second), base, time.Minute, nil},
		{"leeway accepts issue skew", identityTestNow.Add(-30 * time.Second), base, time.Minute, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := identityVerifier(t, source, tc.now, tc.leeway)
			token := signedIdentityToken(t, private, tc.c, "")
			id, err := v.Verify(context.Background(), token)
			if tc.want != nil {
				if !errors.Is(err, tc.want) || id != (Identity{}) {
					t.Fatalf("Verify = id=%+v err=%v, want zero identity and %v", id, err, tc.want)
				}
			} else if err != nil || id.Subject() != "worker-1" {
				t.Fatalf("Verify with leeway = %+v, %v; want success", id, err)
			}
		})
	}
}

func TestIdentity_MalformedPayloadBeforeSignatureDoesNotLeakIdentity(t *testing.T) {
	_, source, private := identityAuthority(t)
	v := identityVerifier(t, source, identityTestNow, 0)
	payload := []byte(`{"iss":"identity-test-issuer","sub":"worker-1","role":"worker","cell":"cell-test","kid":"key-1","iat":1788696000,"exp":1788696300}`)
	token := signedIdentityPayload(private, payload, "")
	if _, err := v.Verify(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), tokenEncoding.EncodeToString(payload)); !errors.Is(err, ErrMalformedCredential) {
		t.Fatalf("non-token payload was accepted: %v", err)
	}
}

func TestIdentity_MaxTokenSizeAndContextResolution(t *testing.T) {
	issuer, source, _ := identityAuthority(t)
	token, err := issuer.Issue(IssueSpec{Subject: "worker-1", Role: RoleWorker, Cell: identityTestCell, Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	v := identityVerifier(t, source, identityTestNow, 0)
	v.maxBytes = len(token) - 1
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, ErrMalformedCredential) {
		t.Fatalf("oversized token = %v, want ErrMalformedCredential", err)
	}
	if _, err := v.Verify(context.Background(), token); !errors.Is(err, ErrMalformedCredential) {
		t.Fatalf("repeated oversized token = %v, want ErrMalformedCredential", err)
	}
	if got := tokenEncoding.EncodeToString([]byte("x")); got != base64.RawURLEncoding.EncodeToString([]byte("x")) {
		t.Fatal("strict raw URL encoding changed unexpectedly")
	}
}
