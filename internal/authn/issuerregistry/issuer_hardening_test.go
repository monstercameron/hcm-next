package issuerregistry

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

func testIssuerDER(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKIXPublicKey: %v", err)
	}
	return der
}

func TestStatusAndJWKSSource_ClosedSetsAndPinningDecisions(t *testing.T) {
	for _, status := range []Status{StatusDraft, StatusActive, StatusSuspended, StatusRetired} {
		if !status.valid() {
			t.Errorf("Status(%q).valid() = false", status)
		}
	}
	if Status("UNKNOWN").valid() || Status("UNKNOWN").terminal() {
		t.Fatal("unknown status was accepted or treated as terminal")
	}
	if !StatusRetired.terminal() || StatusActive.terminal() {
		t.Fatal("terminal status classification is wrong")
	}
	for _, kind := range []JWKSSourceKind{JWKSSourcePinnedKeys, JWKSSourcePinnedBundle} {
		if !kind.valid() {
			t.Errorf("JWKSSourceKind(%q).valid() = false", kind)
		}
	}
	if JWKSSourceKind("LIVE").valid() {
		t.Fatal("unknown JWKS kind was accepted")
	}

	der := testIssuerDER(t)
	for _, source := range []JWKSSource{
		{Kind: JWKSSourcePinnedKeys, PinnedKeys: []PinnedKey{{KeyID: "kid", Algorithm: trustfederation.AlgRS256, PublicKeyDER: der}}},
		{Kind: JWKSSourcePinnedBundle, BundleRef: "bundle", BundleVersion: 2},
	} {
		if !source.pinned() {
			t.Fatalf("source %+v was not recognized as pinned", source)
		}
		if err := source.validate(); err != nil {
			t.Fatalf("source %+v validate: %v", source, err)
		}
	}
	for _, source := range []JWKSSource{
		{},
		{Kind: JWKSSourcePinnedKeys},
		{Kind: JWKSSourcePinnedBundle, BundleRef: "bundle"},
		{Kind: JWKSSourcePinnedBundle, BundleVersion: 2},
	} {
		if err := source.validate(); err == nil {
			t.Fatalf("unpinned source %+v was accepted", source)
		} else if source.Kind != "" && source.Kind != JWKSSourcePinnedKeys && source.Kind != JWKSSourcePinnedBundle && !errors.Is(err, ErrInvalidIssuer) {
			t.Fatalf("unknown source error = %v, want ErrInvalidIssuer", err)
		}
	}
}

func TestPinnedKey_RejectsMalformedAndInvertedValidity(t *testing.T) {
	valid := PinnedKey{KeyID: "kid", Algorithm: trustfederation.AlgRS256, PublicKeyDER: testIssuerDER(t)}
	if err := valid.valid(); err != nil {
		t.Fatalf("valid pinned key: %v", err)
	}
	for _, key := range []PinnedKey{
		{Algorithm: trustfederation.AlgRS256, PublicKeyDER: valid.PublicKeyDER},
		{KeyID: "kid", Algorithm: "HS256", PublicKeyDER: valid.PublicKeyDER},
		{KeyID: "kid", Algorithm: trustfederation.AlgRS256},
		{KeyID: "kid", Algorithm: trustfederation.AlgRS256, PublicKeyDER: []byte("bad")},
		{KeyID: "kid", Algorithm: trustfederation.AlgRS256, PublicKeyDER: valid.PublicKeyDER, NotBefore: time.Unix(2, 0), NotAfter: time.Unix(1, 0)},
	} {
		if err := key.valid(); !errors.Is(err, ErrInvalidIssuer) {
			t.Fatalf("PinnedKey %+v error = %v, want ErrInvalidIssuer", key, err)
		}
	}
}

func TestIssuerClone_DeepCopiesMutableVerificationMaterial(t *testing.T) {
	original := Issuer{
		JWKS:          JWKSSource{PinnedKeys: []PinnedKey{{PublicKeyDER: []byte{1, 2, 3}}}},
		Algorithms:    []trustfederation.Algorithm{trustfederation.AlgRS256},
		ClaimMappings: []ClaimMapping{{SourceClaim: "groups", Target: PrincipalFieldRoles}},
	}
	cloned := original.clone()
	cloned.JWKS.PinnedKeys[0].PublicKeyDER[0] = 9
	cloned.Algorithms[0] = trustfederation.AlgES256
	cloned.ClaimMappings[0].SourceClaim = "changed"
	if original.JWKS.PinnedKeys[0].PublicKeyDER[0] != 1 || original.Algorithms[0] != trustfederation.AlgRS256 || original.ClaimMappings[0].SourceClaim != "groups" {
		t.Fatalf("clone mutation changed original: original=%+v clone=%+v", original, cloned)
	}
}
