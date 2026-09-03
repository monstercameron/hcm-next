package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// CanonicalJSON returns the manifest's canonical serialization: two-space
// indented JSON over the already-sorted [EndpointManifest.Endpoints] slice.
// Every field is a plain string, slice-of-string or integer — never a map —
// so encoding/json's field order (struct declaration order) and this
// package's own sorted slices are the only sources of byte order, and both
// are stable across process runs.
func (m *EndpointManifest) CanonicalJSON() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("manifest: canonical JSON encoding: %w", err)
	}
	return append(b, '\n'), nil
}

// Digest returns the hex-encoded SHA-256 of [EndpointManifest.CanonicalJSON].
// Two [Build] calls in the same or different processes, against the same
// source tree, produce the same digest; [TestTodo_ENDPOINT_001_Property]
// proves this.
func (m *EndpointManifest) Digest() (string, error) {
	b, err := m.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
