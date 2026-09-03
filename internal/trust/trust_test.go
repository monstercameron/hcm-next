package trust_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
)

// fixtureKey is the test signing key. Thirty-two bytes is the minimum the
// verifier accepts.
var fixtureKey = []byte("trust-package-test-signing-key-32b")

const (
	fixtureIssuer   = "https://issuer.test.hcm-next.invalid"
	fixtureAudience = "hcm-next-api"
	otherAudience   = "hcm-next-admin"
)

// baseTime is the fixed instant the tests treat as "now". Nothing in this
// package reads the wall clock, so the tests are deterministic.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// newVerifier returns a verifier whose clock is pinned to at.
func newVerifier(t *testing.T, at time.Time) *trust.HMACVerifier {
	t.Helper()
	v, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      fixtureKey,
		Issuer:   fixtureIssuer,
		Audience: fixtureAudience,
		Now:      func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	return v
}

// validClaims returns a well-formed claim set for the canonical principal.
func validClaims() trust.Claims {
	return trust.Claims{
		Issuer:               fixtureIssuer,
		Audience:             fixtureAudience,
		Subject:              "user-0191f3c4",
		SubjectKind:          "human",
		Tenant:               "acme-corp",
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author", "approver"},
		AuthorityRefs:        []string{"authority:position:vp-engineering"},
		Purposes:             []string{"hcm_operations", "workforce_analytics"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-8fb2c1",
		DelegationRefs:       []string{"delegation:9a12"},
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	}
}

// issue signs claims with the fixture key.
func issue(t *testing.T, v *trust.HMACVerifier, claims trust.Claims) string {
	t.Helper()
	token, err := v.Issue(claims)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return token
}

// TestTodo_TRUST_001 is the TRUST-001 primary test.
//
// GREEN: a verified human/service/agent/integration identity yields an
// immutable principal carrying tenant, authentication method, assurance,
// session and delegation references, plus the server-derived evidence
// identifier that downstream layers quote instead of raw provider claims.
//
// RED: the same test rejects a caller-selected principal/tenant/actor, a wrong
// audience, a wrong issuer, an expired credential and a credential with no
// assurance evidence. Each of those is a separate subtest so a regression
// names itself.
func TestTodo_TRUST_001(t *testing.T) {
	verifier := newVerifier(t, baseTime)
	ctx := context.Background()

	t.Run("verified credential yields the full principal", func(t *testing.T) {
		claims := validClaims()
		principal, err := verifier.Verify(ctx, trust.Credential{
			Scheme: "Bearer",
			Token:  issue(t, verifier, claims),
		})
		if err != nil {
			t.Fatalf("Verify: %v", err)
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
		if !principal.Assurance().AtLeast(trust.AssuranceLow) {
			t.Error("substantial assurance should satisfy a low requirement")
		}
		if principal.Assurance().AtLeast(trust.AssuranceHigh) {
			t.Error("substantial assurance must not satisfy a high requirement")
		}
		if got := principal.SessionRef(); got != claims.SessionRef {
			t.Errorf("session ref = %q, want %q", got, claims.SessionRef)
		}
		if got := principal.DelegationRefs(); len(got) != 1 || got[0] != "delegation:9a12" {
			t.Errorf("delegation refs = %v, want [delegation:9a12]", got)
		}
		if got := principal.AuthorityRefs(); len(got) != 1 {
			t.Errorf("authority refs = %v, want one entry", got)
		}
		if !principal.HasRole("intent_author") {
			t.Error("principal should hold the intent_author role")
		}
		if !principal.AuthorizesPurpose("hcm_operations") {
			t.Error("principal should authorize the hcm_operations purpose")
		}
		if principal.AuthorizesPurpose("marketing_enrichment") {
			t.Error("principal must not authorize an unlisted purpose")
		}
		if principal.EvidenceID() == "" || !strings.HasPrefix(principal.EvidenceID(), "ev:authn:") {
			t.Errorf("evidence id = %q, want an ev:authn: reference", principal.EvidenceID())
		}
		if !strings.HasPrefix(principal.CredentialDigest(), "cred:sha256:") {
			t.Errorf("credential digest = %q, want a cred:sha256: digest", principal.CredentialDigest())
		}
	})

	t.Run("every actor kind authenticates through the same path", func(t *testing.T) {
		for _, tc := range []struct {
			wire string
			want trust.SubjectKind
		}{
			{"human", trust.SubjectKindHuman},
			{"service", trust.SubjectKindService},
			{"agent", trust.SubjectKindAgent},
			{"integration", trust.SubjectKindIntegration},
		} {
			claims := validClaims()
			claims.SubjectKind = tc.wire
			principal, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, claims)})
			if err != nil {
				t.Fatalf("Verify(%s): %v", tc.wire, err)
			}
			if got := principal.SubjectKind(); got != tc.want {
				t.Errorf("subject kind for %q = %v, want %v", tc.wire, got, tc.want)
			}
		}
	})

	t.Run("RED: caller-selected principal, tenant and actor are never trusted", func(t *testing.T) {
		// The reserved names are the entire vocabulary a caller might use to
		// assert identity. None of them is an input to authentication: the
		// only thing Verify reads is the credential.
		for _, name := range []string{
			"x-principal", "X-Tenant", "x-roles", "x-subject",
			"x-organization-scope", "x-purpose", "x-assurance",
			"x-authority", "x-session", "x-delegation", "x-placement",
		} {
			if !trust.IsReservedMetadataKey(name) {
				t.Errorf("%q must be a reserved trusted-context name", name)
			}
		}
		if got := trust.RejectCallerSelectedAuthority([]string{"authorization", "x-request-id", "X-Tenant"}); len(got) != 1 || got[0] != "x-tenant" {
			t.Errorf("RejectCallerSelectedAuthority = %v, want [x-tenant]", got)
		}
		if got := trust.RejectCallerSelectedAuthority([]string{"authorization", "content-type"}); got != nil {
			t.Errorf("RejectCallerSelectedAuthority on a clean request = %v, want nil", got)
		}
	})

	t.Run("RED: wrong audience is rejected", func(t *testing.T) {
		claims := validClaims()
		claims.Audience = otherAudience
		_, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, claims)})
		if !errors.Is(err, trust.ErrWrongAudience) {
			t.Fatalf("Verify = %v, want ErrWrongAudience", err)
		}
	})

	t.Run("RED: a credential for another listener is rejected", func(t *testing.T) {
		claims := validClaims()
		_, err := verifier.Verify(ctx, trust.Credential{
			Token:    issue(t, verifier, claims),
			Audience: otherAudience,
		})
		if !errors.Is(err, trust.ErrWrongAudience) {
			t.Fatalf("Verify = %v, want ErrWrongAudience", err)
		}
	})

	t.Run("RED: wrong issuer is rejected", func(t *testing.T) {
		claims := validClaims()
		claims.Issuer = "https://attacker.example"
		_, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, claims)})
		if !errors.Is(err, trust.ErrWrongIssuer) {
			t.Fatalf("Verify = %v, want ErrWrongIssuer", err)
		}
	})

	t.Run("RED: expired credential is rejected", func(t *testing.T) {
		claims := validClaims()
		token := issue(t, verifier, claims)
		expired := newVerifier(t, time.Unix(claims.ExpiresAtUnix, 0).Add(time.Second))
		if _, err := expired.Verify(ctx, trust.Credential{Token: token}); !errors.Is(err, trust.ErrExpiredCredential) {
			t.Fatalf("Verify after expiry = %v, want ErrExpiredCredential", err)
		}
	})

	t.Run("RED: not-yet-valid credential is rejected", func(t *testing.T) {
		claims := validClaims()
		token := issue(t, verifier, claims)
		early := newVerifier(t, time.Unix(claims.IssuedAtUnix, 0).Add(-time.Hour))
		if _, err := early.Verify(ctx, trust.Credential{Token: token}); !errors.Is(err, trust.ErrExpiredCredential) {
			t.Fatalf("Verify before validity = %v, want ErrExpiredCredential", err)
		}
	})

	t.Run("RED: missing assurance evidence is rejected", func(t *testing.T) {
		claims := validClaims()
		claims.Assurance = ""
		if _, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, claims)}); !errors.Is(err, trust.ErrMissingAssurance) {
			t.Fatalf("Verify without assurance = %v, want ErrMissingAssurance", err)
		}
		claims.Assurance = "very-high-trust-me"
		if _, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, claims)}); !errors.Is(err, trust.ErrMissingAssurance) {
			t.Fatalf("Verify with an invented assurance = %v, want ErrMissingAssurance", err)
		}
	})

	t.Run("RED: absent credential is rejected", func(t *testing.T) {
		if _, err := verifier.Verify(ctx, trust.Credential{}); !errors.Is(err, trust.ErrNoCredential) {
			t.Fatalf("Verify without a credential = %v, want ErrNoCredential", err)
		}
	})

	t.Run("context helpers carry exactly what was verified", func(t *testing.T) {
		principal, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, validClaims())})
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if _, ok := trust.FromContext(ctx); ok {
			t.Fatal("a bare context must not carry a principal")
		}
		if _, err := trust.MustFromContext(ctx); !errors.Is(err, trust.ErrNoPrincipal) {
			t.Fatalf("MustFromContext on a bare context = %v, want ErrNoPrincipal", err)
		}
		withPrincipal := trust.WithPrincipal(ctx, principal)
		got, ok := trust.FromContext(withPrincipal)
		if !ok {
			t.Fatal("FromContext should find the principal that was installed")
		}
		if got.Fingerprint() != principal.Fingerprint() {
			t.Error("FromContext returned a different principal than WithPrincipal installed")
		}
	})
}

// TestTodo_TRUST_001_Security is the TRUST-001 security test. It attacks the
// credential itself: a forged signature, a re-signed payload, a tampered
// payload, a token from a different key and an oversized token must all fail,
// and no failure may disclose which check rejected it to a caller.
func TestTodo_TRUST_001_Security(t *testing.T) {
	verifier := newVerifier(t, baseTime)
	ctx := context.Background()
	valid := issue(t, verifier, validClaims())

	t.Run("tampered payload does not verify", func(t *testing.T) {
		parts := strings.Split(valid, ".")
		if len(parts) != 3 {
			t.Fatalf("token has %d parts, want 3", len(parts))
		}
		claims := validClaims()
		claims.Tenant = "victim-corp"
		payload, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + parts[2]
		if _, err := verifier.Verify(ctx, trust.Credential{Token: forged}); !errors.Is(err, trust.ErrInvalidCredential) {
			t.Fatalf("Verify(tampered) = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("token signed with another key does not verify", func(t *testing.T) {
		other, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
			Key:      []byte("a-completely-different-32-byte-key"),
			Issuer:   fixtureIssuer,
			Audience: fixtureAudience,
			Now:      func() time.Time { return baseTime },
		})
		if err != nil {
			t.Fatalf("NewHMACVerifier: %v", err)
		}
		foreign := issue(t, other, validClaims())
		if _, err := verifier.Verify(ctx, trust.Credential{Token: foreign}); !errors.Is(err, trust.ErrInvalidCredential) {
			t.Fatalf("Verify(foreign key) = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("malformed tokens do not verify", func(t *testing.T) {
		for _, token := range []string{
			"not-a-token",
			"hcmn1.only-two-parts",
			"hcmn1..",
			"hcmn2." + strings.SplitN(valid, ".", 2)[1],
			valid + ".extra",
			strings.ToUpper(valid),
		} {
			if _, err := verifier.Verify(ctx, trust.Credential{Token: token}); err == nil {
				t.Errorf("Verify(%q) succeeded, want a failure", token)
			}
		}
	})

	t.Run("unknown claim keys do not verify", func(t *testing.T) {
		// Strict claim decoding matters: a token that smuggles an extra key
		// past the parser is a token whose meaning depends on the reader.
		payload := []byte(`{"iss":"` + fixtureIssuer + `","aud":"` + fixtureAudience +
			`","sub":"u","sub_kind":"human","tenant":"acme-corp","authn_method":"bearer_token",` +
			`"assurance":"high","session_ref":"s","iat":1,"exp":2,"escalate":true}`)
		body := base64.RawURLEncoding.EncodeToString(payload)
		// Sign it correctly so that only the unknown key can reject it.
		signed, err := signWithFixtureKey(body)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if _, err := verifier.Verify(ctx, trust.Credential{Token: signed}); !errors.Is(err, trust.ErrInvalidCredential) {
			t.Fatalf("Verify(unknown claim) = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("oversized token is rejected before parsing", func(t *testing.T) {
		bounded, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
			Key:           fixtureKey,
			Issuer:        fixtureIssuer,
			Audience:      fixtureAudience,
			Now:           func() time.Time { return baseTime },
			MaxTokenBytes: 64,
		})
		if err != nil {
			t.Fatalf("NewHMACVerifier: %v", err)
		}
		if _, err := bounded.Verify(ctx, trust.Credential{Token: valid}); !errors.Is(err, trust.ErrInvalidCredential) {
			t.Fatalf("Verify(oversized) = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("non-bearer scheme is rejected", func(t *testing.T) {
		if _, err := verifier.Verify(ctx, trust.Credential{Scheme: "Basic", Token: valid}); !errors.Is(err, trust.ErrUnsupportedScheme) {
			t.Fatalf("Verify(Basic) = %v, want ErrUnsupportedScheme", err)
		}
	})

	t.Run("a principal never carries the raw credential", func(t *testing.T) {
		principal, err := verifier.Verify(ctx, trust.Credential{Token: valid})
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		rendered := principal.String() + "|" + principal.CredentialDigest() + "|" + principal.EvidenceID()
		if strings.Contains(rendered, valid) {
			t.Error("the principal's rendered form leaks the raw credential")
		}
		body := strings.SplitN(valid, ".", 3)[1]
		if strings.Contains(rendered, body) {
			t.Error("the principal's rendered form leaks the credential payload")
		}
	})
}

// TestTodo_TRUST_001_Mutation is the TRUST-001 mutation test. It is not about
// input validation: it asserts that a principal, once created, cannot be
// changed, and that the derived evidence identifier and fingerprint actually
// depend on every trusted field. A mutation that drops a field from the
// fingerprint, or that lets an accessor hand out the backing array, fails
// here.
func TestTodo_TRUST_001_Mutation(t *testing.T) {
	verifier := newVerifier(t, baseTime)
	ctx := context.Background()

	principal, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, validClaims())})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	t.Run("accessors hand out copies", func(t *testing.T) {
		roles := principal.Roles()
		roles[0] = "tenant_admin"
		if principal.Roles()[0] == "tenant_admin" {
			t.Error("mutating the returned role slice changed the principal")
		}
		purposes := principal.Purposes()
		purposes[0] = "anything"
		if principal.AuthorizesPurpose("anything") {
			t.Error("mutating the returned purpose slice changed the principal")
		}
		delegations := principal.DelegationRefs()
		if len(delegations) > 0 {
			delegations[0] = "delegation:forged"
			if principal.DelegationRefs()[0] == "delegation:forged" {
				t.Error("mutating the returned delegation slice changed the principal")
			}
		}
	})

	t.Run("every trusted field participates in the fingerprint", func(t *testing.T) {
		base := validClaims()
		mutations := map[string]func(*trust.Claims){
			"tenant":        func(c *trust.Claims) { c.Tenant = "other-corp" },
			"subject":       func(c *trust.Claims) { c.Subject = "user-different" },
			"subject kind":  func(c *trust.Claims) { c.SubjectKind = "service" },
			"org scope":     func(c *trust.Claims) { c.OrganizationScopeID = "org-emea" },
			"roles":         func(c *trust.Claims) { c.Roles = []string{"tenant_admin"} },
			"authority":     func(c *trust.Claims) { c.AuthorityRefs = []string{"authority:position:ceo"} },
			"purposes":      func(c *trust.Claims) { c.Purposes = []string{"hcm_operations"} },
			"assurance":     func(c *trust.Claims) { c.Assurance = "high" },
			"session":       func(c *trust.Claims) { c.SessionRef = "session-other" },
			"delegation":    func(c *trust.Claims) { c.DelegationRefs = nil },
			"issued at":     func(c *trust.Claims) { c.IssuedAtUnix = base.IssuedAtUnix - 30 },
			"expires at":    func(c *trust.Claims) { c.ExpiresAtUnix = base.ExpiresAtUnix + 30 },
			"authn method":  func(c *trust.Claims) { c.AuthenticationMethod = "mutual_tls" },
			"credential id": func(c *trust.Claims) { c.Subject = base.Subject + "-2" },
		}
		for name, mutate := range mutations {
			claims := base
			mutate(&claims)
			mutated, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, claims)})
			if err != nil {
				t.Fatalf("Verify after mutating %s: %v", name, err)
			}
			if mutated.Fingerprint() == principal.Fingerprint() {
				t.Errorf("mutating %s left the fingerprint unchanged", name)
			}
			if mutated.EvidenceID() == principal.EvidenceID() {
				t.Errorf("mutating %s left the evidence id unchanged", name)
			}
		}
	})

	t.Run("the same credential always derives the same evidence id", func(t *testing.T) {
		token := issue(t, verifier, validClaims())
		first, err := verifier.Verify(ctx, trust.Credential{Token: token})
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		second, err := verifier.Verify(ctx, trust.Credential{Token: token})
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if first.EvidenceID() != second.EvidenceID() {
			t.Error("the same credential derived two different evidence identifiers")
		}
		if first.Fingerprint() != second.Fingerprint() {
			t.Error("the same credential derived two different fingerprints")
		}
	})

	t.Run("role ordering does not change identity", func(t *testing.T) {
		claims := validClaims()
		claims.Roles = []string{"approver", "intent_author", "approver"}
		reordered, err := verifier.Verify(ctx, trust.Credential{Token: issue(t, verifier, claims)})
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if got := reordered.Roles(); len(got) != 2 {
			t.Fatalf("roles = %v, want two canonical entries", got)
		}
	})

	t.Run("NewPrincipal fails closed on an incomplete result", func(t *testing.T) {
		valid := trust.PrincipalSpec{
			Tenant:               "acme-corp",
			Subject:              "user-1",
			SubjectKind:          trust.SubjectKindHuman,
			AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance:            trust.AssuranceHigh,
			SessionRef:           "session-1",
			IssuedAt:             baseTime,
			ExpiresAt:            baseTime.Add(time.Hour),
			CredentialDigest:     "cred:sha256:abc",
		}
		if _, err := trust.NewPrincipal(valid); err != nil {
			t.Fatalf("NewPrincipal(valid) = %v", err)
		}
		for name, mutate := range map[string]func(*trust.PrincipalSpec){
			"no tenant":     func(s *trust.PrincipalSpec) { s.Tenant = "" },
			"no subject":    func(s *trust.PrincipalSpec) { s.Subject = "" },
			"no kind":       func(s *trust.PrincipalSpec) { s.SubjectKind = trust.SubjectKindUnspecified },
			"no method":     func(s *trust.PrincipalSpec) { s.AuthenticationMethod = trust.AuthenticationMethodUnspecified },
			"no assurance":  func(s *trust.PrincipalSpec) { s.Assurance = trust.AssuranceUnspecified },
			"no session":    func(s *trust.PrincipalSpec) { s.SessionRef = "" },
			"no validity":   func(s *trust.PrincipalSpec) { s.ExpiresAt = time.Time{} },
			"inverted":      func(s *trust.PrincipalSpec) { s.ExpiresAt = s.IssuedAt.Add(-time.Hour) },
			"no credential": func(s *trust.PrincipalSpec) { s.CredentialDigest = "" },
		} {
			spec := valid
			mutate(&spec)
			if _, err := trust.NewPrincipal(spec); err == nil {
				t.Errorf("NewPrincipal(%s) succeeded, want a failure", name)
			}
		}
	})
}

// signWithFixtureKey signs an already-encoded payload body with the fixture
// key, using the verifier's own Issue path indirectly is impossible here
// because the payload is deliberately not a valid Claims value. The token
// layout is prefix "." body "." signature over "prefix.body".
func signWithFixtureKey(body string) (string, error) {
	mac := hmacSHA256(fixtureKey, "hcmn1."+body)
	return "hcmn1." + body + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}
