package release_test

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
)

func TestTodo_SECARCH_005(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 41
	private := ed25519.NewKeyFromSeed(seed)
	provider := &releaseCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key:v1": private}}
	source := custodySource(t, provider, "v1", private)
	inputs := newReleaseInputs(t)
	writeSignedReleaseStatement(t, inputs, source)
	out := filepath.Join(inputs.root, "bundle")

	manifest, err := release.BuildWithKeySource(inputs.root, inputs.options(out), source)
	if err != nil {
		t.Fatalf("BuildWithKeySource: %v", err)
	}
	if manifest.Signature == nil || manifest.Signature.PublicKey != source.PublicKey() {
		t.Fatalf("manifest signature = %+v, want custody public key %q", manifest.Signature, source.PublicKey())
	}
	if manifest.Signature.PublicKey == release.DevFixturePublicKey {
		t.Fatal("custody-backed bundle unexpectedly used the development fixture key")
	}
	if _, err := release.VerifyBundle(out, release.VerifyOptions{TrustedPublicKeys: map[string]bool{source.PublicKey(): true}}); err != nil {
		t.Fatalf("VerifyBundle with custody trust anchor: %v", err)
	}
}

func TestTodo_SECARCH_005_Golden(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 42
	private := ed25519.NewKeyFromSeed(seed)
	provider := &releaseCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key:v1": private}}
	source := custodySource(t, provider, "v1", private)
	inputs := newReleaseInputs(t)
	writeSignedReleaseStatement(t, inputs, source)

	firstOut := filepath.Join(inputs.root, "bundle-first")
	secondOut := filepath.Join(inputs.root, "bundle-second")
	first, err := release.BuildWithKeySource(inputs.root, inputs.options(firstOut), source)
	if err != nil {
		t.Fatalf("first BuildWithKeySource: %v", err)
	}
	second, err := release.BuildWithKeySource(inputs.root, inputs.options(secondOut), source)
	if err != nil {
		t.Fatalf("second BuildWithKeySource: %v", err)
	}
	firstBytes, err := os.ReadFile(filepath.Join(firstOut, release.ManifestFileName))
	if err != nil {
		t.Fatalf("read first manifest: %v", err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(secondOut, release.ManifestFileName))
	if err != nil {
		t.Fatalf("read second manifest: %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("identical custody inputs produced different manifest bytes")
	}
	firstDigest, err := first.CanonicalDigest()
	if err != nil {
		t.Fatalf("first CanonicalDigest: %v", err)
	}
	secondDigest, err := second.CanonicalDigest()
	if err != nil {
		t.Fatalf("second CanonicalDigest: %v", err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("manifest digest changed for identical custody inputs: %s != %s", firstDigest, secondDigest)
	}
}

func TestTodo_SECARCH_005_Security(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 43
	private := ed25519.NewKeyFromSeed(seed)
	provider := &releaseCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key:v1": private}}
	source := custodySource(t, provider, "v1", private)
	inputs := newReleaseInputs(t)
	writeSignedReleaseStatement(t, inputs, source)
	out := filepath.Join(inputs.root, "bundle")
	if _, err := release.BuildWithKeySource(inputs.root, inputs.options(out), source); err != nil {
		t.Fatalf("BuildWithKeySource: %v", err)
	}
	if _, err := release.VerifyBundle(out, release.VerifyOptions{}); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("default development trust unexpectedly admitted custody bundle: %v", err)
	}
	if _, err := release.VerifyBundle(out, release.VerifyOptions{TrustedPublicKeys: map[string]bool{release.DevFixturePublicKey: true}}); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("development fixture trust unexpectedly admitted custody bundle: %v", err)
	}
}
