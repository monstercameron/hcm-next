package toolinventory_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/toolinventory"
	"gopkg.in/yaml.v3"
)

// TestTodo_TOOL_025_Conformance proves end-to-end fidelity: a freshly
// generated manifest survives a Marshal/Unmarshal round trip unchanged,
// and the checked-in definitions/toolchain/tool-inventory.yaml both
// parses into a schema-valid Manifest and carries digests that still
// verify against the source files that pin them right now.
func TestTodo_TOOL_025_Conformance(t *testing.T) {
	root := repoRoot(t)

	generated, err := toolinventory.Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	encoded, err := toolinventory.Marshal(generated)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundTripped toolinventory.Manifest
	if err := yaml.Unmarshal(encoded, &roundTripped); err != nil {
		t.Fatalf("Unmarshal(Marshal(generated)): %v", err)
	}
	if !toolinventory.Equal(generated, roundTripped) {
		t.Fatal("Marshal/Unmarshal round trip did not preserve the manifest")
	}

	checkedInPath := filepath.Join(root, "definitions", "toolchain", "tool-inventory.yaml")
	checkedIn, err := toolinventory.Load(checkedInPath)
	if err != nil {
		t.Fatalf("Load(%s): %v", checkedInPath, err)
	}
	if err := checkedIn.Validate(); err != nil {
		t.Fatalf("checked-in manifest violates the schema: %v", err)
	}
	if err := toolinventory.VerifyDigests(root, checkedIn); err != nil {
		t.Fatalf("checked-in manifest digest verification failed: %v", err)
	}
}
