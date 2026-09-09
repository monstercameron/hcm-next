package endpointmanifest_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
)

func writeManifest(t *testing.T, path string, m *manifest.EndpointManifest) {
	t.Helper()
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshaling fixture manifest: %v", err)
	}
	writeRaw(t, path, string(raw))
}

func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture file %s: %v", path, err)
	}
}
