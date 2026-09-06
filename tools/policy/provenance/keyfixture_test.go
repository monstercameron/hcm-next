package provenance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/provenance"
)

func TestLoadSigningKeyFixtureLoadsTheCheckedInDevKey(t *testing.T) {
	priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	if len(priv) == 0 {
		t.Fatal("LoadSigningKeyFixture returned an empty private key")
	}
}

func TestLoadSigningKeyFixtureLoadsTheGateevidenceYAMLKey(t *testing.T) {
	priv, err := provenance.LoadSigningKeyFixture(filepath.Join("..", "..", "planning", "gateevidence", "testdata", "dev-signing-key.yaml"))
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture(gateevidence YAML): %v", err)
	}
	if len(priv) != 64 {
		t.Fatalf("private key length = %d, want 64", len(priv))
	}
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "key.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestLoadSigningKeyFixtureRejectsMissingFile(t *testing.T) {
	if _, err := provenance.LoadSigningKeyFixture(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("expected an error for a missing fixture file")
	}
}

func TestLoadSigningKeyFixtureRejectsMalformedJSON(t *testing.T) {
	path := writeFixture(t, "{not valid json")
	if _, err := provenance.LoadSigningKeyFixture(path); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestLoadSigningKeyFixtureRejectsUnsupportedAlgorithm(t *testing.T) {
	path := writeFixture(t, `{"schema_version":1,"algorithm":"rsa","public_key":"aa","private_key":"bb"}`)
	if _, err := provenance.LoadSigningKeyFixture(path); err == nil {
		t.Fatal("expected an error for an unsupported algorithm")
	}
}

func TestLoadSigningKeyFixtureRejectsBadPrivateKeyHex(t *testing.T) {
	path := writeFixture(t, `{"schema_version":1,"algorithm":"ed25519","public_key":"aa","private_key":"not-hex"}`)
	if _, err := provenance.LoadSigningKeyFixture(path); err == nil {
		t.Fatal("expected an error for non-hex private_key")
	}
}

func TestLoadSigningKeyFixtureRejectsWrongLengthPrivateKey(t *testing.T) {
	path := writeFixture(t, `{"schema_version":1,"algorithm":"ed25519","public_key":"aa","private_key":"aabbcc"}`)
	if _, err := provenance.LoadSigningKeyFixture(path); err == nil {
		t.Fatal("expected an error for a too-short private_key")
	}
}

func TestLoadSigningKeyFixtureRejectsPublicKeyMismatch(t *testing.T) {
	// A structurally valid 64-byte private key, but the declared
	// public_key field does not match the key actually embedded in it -
	// this must be caught, not silently trusted.
	content := `{"schema_version":1,"algorithm":"ed25519","public_key":"` + strings.Repeat("00", 32) + `","private_key":"` + strings.Repeat("11", 64) + `"}`
	path := writeFixture(t, content)
	if _, err := provenance.LoadSigningKeyFixture(path); err == nil {
		t.Fatal("expected an error when declared public_key does not match the key embedded in private_key")
	}
}
