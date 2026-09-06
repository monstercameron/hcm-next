package provenance

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SigningKeyFixture is the on-disk shape of a development signing key: the
// same schema_version/algorithm/public_key/private_key fields
// tools/planning/gateevidence's testdata/dev-signing-key.yaml uses. The
// loader accepts both that simple YAML fixture and JSON fixtures. THIS SHAPE
// IS FOR TEST/DEVELOPMENT FIXTURES ONLY - see
// testdata/dev-signing-key.json's own warning comment. Nothing in this
// package treats a SigningKeyFixture as carrying any production signing
// authority.
type SigningKeyFixture struct {
	SchemaVersion int    `json:"schema_version"`
	Algorithm     string `json:"algorithm"`
	PublicKey     string `json:"public_key"`
	PrivateKey    string `json:"private_key"`
}

// LoadSigningKeyFixture reads and parses a SigningKeyFixture JSON or simple
// YAML file at path, decodes its hex private key, and confirms the embedded
// public half of that private key matches the fixture's declared public_key
// field (catching a hand-edited or truncated fixture before it ever signs
// anything). It returns the usable ed25519.PrivateKey.
func LoadSigningKeyFixture(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("provenance: reading signing key fixture %s: %w", path, err)
	}
	var f SigningKeyFixture
	if err := json.Unmarshal(data, &f); err != nil {
		var yamlErr error
		f, yamlErr = parseSimpleYAMLFixture(data)
		if yamlErr != nil {
			return nil, fmt.Errorf("provenance: parsing signing key fixture %s: %w", path, err)
		}
	}
	if f.SchemaVersion != 1 {
		return nil, fmt.Errorf("provenance: signing key fixture %s: unsupported schema_version %d", path, f.SchemaVersion)
	}
	if f.Algorithm != AlgorithmEd25519 {
		return nil, fmt.Errorf("provenance: signing key fixture %s: unsupported algorithm %q", path, f.Algorithm)
	}
	privBytes, err := hex.DecodeString(f.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("provenance: signing key fixture %s: decoding private_key: %w", path, err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("provenance: signing key fixture %s: private_key has %d bytes, want %d", path, len(privBytes), ed25519.PrivateKeySize)
	}
	priv := ed25519.PrivateKey(privBytes)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("provenance: signing key fixture %s: private key's Public() did not return an ed25519.PublicKey", path)
	}
	if gotPub := hex.EncodeToString(pub); gotPub != f.PublicKey {
		return nil, fmt.Errorf("provenance: signing key fixture %s: declared public_key %q does not match the key embedded in private_key (%q)", path, f.PublicKey, gotPub)
	}
	return priv, nil
}

// parseSimpleYAMLFixture parses the four scalar fields used by the existing
// gateevidence development fixture. Keeping this deliberately narrow avoids
// adding a YAML dependency to the provenance package while allowing provgen
// to use the exact key fixture already used by gateevidence.
func parseSimpleYAMLFixture(data []byte) (SigningKeyFixture, error) {
	values := make(map[string]string, 4)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return SigningKeyFixture{}, fmt.Errorf("invalid YAML line %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return SigningKeyFixture{}, fmt.Errorf("invalid YAML field %q", key)
		}
		values[key] = strings.Trim(value, "\"'")
	}

	schemaVersion, err := strconv.Atoi(values["schema_version"])
	if err != nil {
		return SigningKeyFixture{}, fmt.Errorf("invalid schema_version: %w", err)
	}
	return SigningKeyFixture{
		SchemaVersion: schemaVersion,
		Algorithm:     values["algorithm"],
		PublicKey:     values["public_key"],
		PrivateKey:    values["private_key"],
	}, nil
}
