package provenance_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/provenance"
)

const devSigningKeyFixture = "testdata/dev-signing-key.json"

func goldenStatement() provenance.Statement {
	flags := []string{"-trimpath=true", "CGO_ENABLED=0"}
	return provenance.Statement{
		SchemaVersion: provenance.SchemaVersion,
		PredicateType: provenance.PredicateType,
		GeneratedAt:   "2026-09-05T00:00:00Z",
		Subjects: []provenance.Subject{
			{Name: "hcmnext", SHA256: strings.Repeat("ab", 32)},
		},
		Builder: provenance.Builder{ID: provenance.BuilderID},
		Source: provenance.SourceRef{
			Repository: provenance.RootModulePath,
			Ref:        "unknown",
			Commit:     "unknown",
		},
		BuildConfig: provenance.BuildConfig{
			GoVersion:    "go1.26.3",
			GOOS:         "windows",
			GOARCH:       "arm64",
			Flags:        flags,
			ConfigDigest: provenance.ConfigDigest("go1.26.3", "windows", "arm64", flags),
		},
		SBOM: provenance.SBOMReference{
			Path:   provenance.DefaultSBOMPath,
			SHA256: strings.Repeat("ef", 32),
		},
	}
}

// TestTodo_TOOL_018_Golden is TOOL-018's GOLDEN matrix test: fixed inputs,
// signed with the checked-in dev key fixture, must reproduce the exact
// canonical digest and Ed25519 signature byte-for-byte on every run - a
// regression in field order, JSON encoding, or the signing convention
// itself would change these values.
func TestTodo_TOOL_018_Golden(t *testing.T) {
	const wantConfigDigest = "1d181fb921db9e2e0c270843fd7f07d0416337b7c78b1238625df8fa7b1fb217"
	const wantCanonicalDigest = "311587bdbe2e8d268b58650c0837d6e32894f7365a667f69f440c1cb0245d453"
	const wantPublicKey = "93019e7b15fc44dbfb7f36e105e465486e5d5a6ddec109abda44507e0d45332b"
	const wantSignature = "54ea8b076f4a367501702d61aa9150dc94461d23f3f743da35ca8594237cddb2dc8c1c268c31cf5f3898f6d153d0d6b220e0880b13c041dcc9d5c7371523e30e"

	s := goldenStatement()
	if s.BuildConfig.ConfigDigest != wantConfigDigest {
		t.Fatalf("ConfigDigest = %s, want %s", s.BuildConfig.ConfigDigest, wantConfigDigest)
	}

	digest, err := s.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest != wantCanonicalDigest {
		t.Fatalf("CanonicalDigest() = %s, want %s", digest, wantCanonicalDigest)
	}

	priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	signed, err := provenance.SignStatement(priv, s, devSigningKeyFixture)
	if err != nil {
		t.Fatalf("SignStatement: %v", err)
	}
	if signed.Signature.PublicKey != wantPublicKey {
		t.Errorf("Signature.PublicKey = %s, want %s", signed.Signature.PublicKey, wantPublicKey)
	}
	if signed.Signature.Value != wantSignature {
		t.Errorf("Signature.Value = %s, want %s", signed.Signature.Value, wantSignature)
	}
	if signed.Signature.Algorithm != provenance.AlgorithmEd25519 {
		t.Errorf("Signature.Algorithm = %s, want %s", signed.Signature.Algorithm, provenance.AlgorithmEd25519)
	}

	ok, err := provenance.VerifyStatementSignature(signed)
	if err != nil {
		t.Fatalf("VerifyStatementSignature: %v", err)
	}
	if !ok {
		t.Fatal("the golden signature must verify against the golden statement")
	}
}

func TestCanonicalDigestExcludesSignatureAndIsStableAcrossReSerialization(t *testing.T) {
	s := goldenStatement()
	unsignedDigest, err := s.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest (unsigned): %v", err)
	}

	priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	signed, err := provenance.SignStatement(priv, s, devSigningKeyFixture)
	if err != nil {
		t.Fatalf("SignStatement: %v", err)
	}

	signedDigest, err := signed.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest (signed): %v", err)
	}
	if signedDigest != unsignedDigest {
		t.Fatalf("CanonicalDigest changed after signing: unsigned=%s signed=%s; a signature must never cover its own bytes", unsignedDigest, signedDigest)
	}
}

func TestSignDigestAndVerifyDigestSignatureRoundTrip(t *testing.T) {
	priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	digest := strings.Repeat("11", 32)

	sig, err := provenance.SignDigest(priv, digest)
	if err != nil {
		t.Fatalf("SignDigest: %v", err)
	}

	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatalf("priv.Public() did not return an ed25519.PublicKey")
	}
	pubHex := hex.EncodeToString(pub)

	valid, err := provenance.VerifyDigestSignature(pubHex, digest, sig)
	if err != nil {
		t.Fatalf("VerifyDigestSignature: %v", err)
	}
	if !valid {
		t.Fatal("signature produced by SignDigest must verify against VerifyDigestSignature")
	}

	tamperedDigest := strings.Repeat("22", 32)
	valid, err = provenance.VerifyDigestSignature(pubHex, tamperedDigest, sig)
	if err != nil {
		t.Fatalf("VerifyDigestSignature (tampered digest): %v", err)
	}
	if valid {
		t.Fatal("signature over one digest must not verify against a different digest")
	}
}

func TestSignDigestRejectsMalformedPrivateKeyAndShortSignature(t *testing.T) {
	if _, err := provenance.SignDigest(ed25519.PrivateKey{1}, strings.Repeat("00", 32)); err == nil {
		t.Fatal("SignDigest accepted a malformed private key without an error")
	}

	priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	pub := hex.EncodeToString(priv.Public().(ed25519.PublicKey))
	if _, err := provenance.VerifyDigestSignature(pub, strings.Repeat("00", 32), "00"); err == nil {
		t.Fatal("VerifyDigestSignature accepted a short signature without an error")
	}
}

func TestVerifyDigestSignatureRejectsMalformedInput(t *testing.T) {
	if _, err := provenance.VerifyDigestSignature("not-hex", strings.Repeat("00", 32), strings.Repeat("00", 64)); err == nil {
		t.Error("expected an error for a non-hex public key")
	}
	if _, err := provenance.VerifyDigestSignature(strings.Repeat("00", 4), strings.Repeat("00", 32), strings.Repeat("00", 64)); err == nil {
		t.Error("expected an error for a too-short public key")
	}
	if _, err := provenance.VerifyDigestSignature(strings.Repeat("00", 32), "not-hex", strings.Repeat("00", 64)); err == nil {
		t.Error("expected an error for a non-hex digest")
	}
	if _, err := provenance.VerifyDigestSignature(strings.Repeat("00", 32), strings.Repeat("00", 32), "not-hex"); err == nil {
		t.Error("expected an error for a non-hex signature")
	}
}

func TestVerifyStatementSignatureRejectsMissingOrUnsupportedSignature(t *testing.T) {
	s := goldenStatement()

	if _, err := provenance.VerifyStatementSignature(s); err == nil {
		t.Error("expected an error for a statement with no signature")
	}

	s.Signature = &provenance.Signature{Algorithm: "rsa", PublicKey: "aa", Value: "bb"}
	if _, err := provenance.VerifyStatementSignature(s); err == nil {
		t.Error("expected an error for an unsupported signature algorithm")
	}
}

func TestSignStatementDetectsTampering(t *testing.T) {
	priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	signed, err := provenance.SignStatement(priv, goldenStatement(), devSigningKeyFixture)
	if err != nil {
		t.Fatalf("SignStatement: %v", err)
	}

	ok, err := provenance.VerifyStatementSignature(signed)
	if err != nil || !ok {
		t.Fatalf("original signed statement must verify: ok=%v err=%v", ok, err)
	}

	tampered := signed
	tampered.Subjects = append([]provenance.Subject(nil), signed.Subjects...)
	tampered.Subjects[0].SHA256 = strings.Repeat("99", 32)

	ok, err = provenance.VerifyStatementSignature(tampered)
	if err != nil {
		t.Fatalf("VerifyStatementSignature (tampered): %v", err)
	}
	if ok {
		t.Fatal("a statement with a tampered subject digest must not verify against the original signature")
	}
}
