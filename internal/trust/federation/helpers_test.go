package federation_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

const (
	tenantAcme    = values.TenantId("acme-corp")
	tenantOther   = values.TenantId("other-corp")
	issuerAcme    = "https://login.acme.invalid/"
	issuerOther   = "https://login.other-corp.invalid/"
	audienceUnder = "hcm-next-api"
)

// baseTime is the fixed instant every test in this package treats as "now".
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// fatalHelper is the subset of *testing.T and *testing.F this file's setup
// helpers need. *testing.F deliberately does not implement testing.TB, so a
// helper shared between an ordinary test and a fuzz target's seed-corpus
// setup is typed against this narrower interface instead.
type fatalHelper interface {
	Helper()
	Fatalf(format string, args ...any)
}

// testKeys bundles one signing keypair per algorithm plus the [federation.KeySource]
// built from their public halves, so a test can mint an assertion under any
// of the three algorithms and validate it against the same source.
type testKeys struct {
	rsaKey *rsa.PrivateKey
	ecKey  *ecdsa.PrivateKey
	edPub  ed25519.PublicKey
	edPriv ed25519.PrivateKey
	source *federation.StaticKeySource
}

func newTestKeys(t fatalHelper) *testKeys {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}

	source := federation.NewStaticKeySource().
		WithKey(issuerAcme, federation.SigningKey{ID: "acme-rsa-1", Algorithm: federation.AlgRS256, Public: &rsaKey.PublicKey}).
		WithKey(issuerAcme, federation.SigningKey{ID: "acme-ec-1", Algorithm: federation.AlgES256, Public: &ecKey.PublicKey}).
		WithKey(issuerOther, federation.SigningKey{ID: "other-ed-1", Algorithm: federation.AlgEdDSA, Public: edPub})

	return &testKeys{rsaKey: rsaKey, ecKey: ecKey, edPub: edPub, edPriv: edPriv, source: source}
}

func validAcmeClaims() federation.Claims {
	return federation.Claims{
		Issuer:              issuerAcme,
		Audience:            audienceUnder,
		Subject:             "user-0191f3c4",
		SubjectKind:         "human",
		Tenant:              string(tenantAcme),
		OrganizationScopeID: "org-north-america",
		Roles:               []string{"intent_author", "approver"},
		AuthorityRefs:       []string{"authority:position:vp-engineering"},
		Purposes:            []string{"hcm_operations", "workforce_analytics"},
		Assurance:           "substantial",
		DelegationRefs:      []string{"delegation:9a12"},
		IssuedAtUnix:        baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:       baseTime.Add(time.Hour).Unix(),
	}
}

func validOtherClaims() federation.Claims {
	c := validAcmeClaims()
	c.Issuer = issuerOther
	c.Tenant = string(tenantOther)
	c.Subject = "user-other-1"
	return c
}

// joseHeaderJSON returns the raw JSON of a JWS protected header.
func joseHeaderJSON(t fatalHelper, alg, kid, typ string) []byte {
	t.Helper()
	h := map[string]string{"alg": alg}
	if kid != "" {
		h["kid"] = kid
	}
	if typ != "" {
		h["typ"] = typ
	}
	b, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	return b
}

// signRS256 signs claims with the RSA fixture key under kid and returns the
// compact JWS text.
func (k *testKeys) signRS256(t fatalHelper, claims federation.Claims, kid string) string {
	t.Helper()
	return k.sign(t, "RS256", kid, "JWT", claims, func(signingInput string) []byte {
		sum := sha256.Sum256([]byte(signingInput))
		sig, err := rsa.SignPKCS1v15(rand.Reader, k.rsaKey, crypto.SHA256, sum[:])
		if err != nil {
			t.Fatalf("rsa.SignPKCS1v15: %v", err)
		}
		return sig
	})
}

// signES256 signs claims with the ECDSA fixture key under kid, producing the
// raw R||S signature format JWS ES256 requires (not ASN.1 DER).
func (k *testKeys) signES256(t fatalHelper, claims federation.Claims, kid string) string {
	t.Helper()
	return k.sign(t, "ES256", kid, "JWT", claims, func(signingInput string) []byte {
		sum := sha256.Sum256([]byte(signingInput))
		r, s, err := ecdsa.Sign(rand.Reader, k.ecKey, sum[:])
		if err != nil {
			t.Fatalf("ecdsa.Sign: %v", err)
		}
		return fixedWidth(r, 32, s, 32)
	})
}

// signEdDSA signs claims with the Ed25519 fixture key under kid.
func (k *testKeys) signEdDSA(t fatalHelper, claims federation.Claims, kid string) string {
	t.Helper()
	return k.sign(t, "EdDSA", kid, "JWT", claims, func(signingInput string) []byte {
		return ed25519.Sign(k.edPriv, []byte(signingInput))
	})
}

func (k *testKeys) sign(t fatalHelper, alg, kid, typ string, claims federation.Claims, signer func(signingInput string) []byte) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	header := joseHeaderJSON(t, alg, kid, typ)
	signingInput := b64url(header) + "." + b64url(payload)
	sig := signer(signingInput)
	return signingInput + "." + b64url(sig)
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// fixedWidth concatenates two big.Int values as fixed-width big-endian byte
// strings, which is the JWS ES256 signature encoding.
func fixedWidth(r *big.Int, rWidth int, s *big.Int, sWidth int) []byte {
	out := make([]byte, rWidth+sWidth)
	r.FillBytes(out[:rWidth])
	s.FillBytes(out[rWidth:])
	return out
}
