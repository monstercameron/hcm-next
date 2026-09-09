package toolinventory_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/toolinventory"
)

// TestToolchainSupplyChainManifestRejectsUntrackedToolInput is the
// TOOL-025 primary test. RED: an Entry missing any of the seven required
// governance fields (or a Manifest containing a duplicate tool name) is
// rejected by Validate. GREEN: Generate(root)'s output is itself complete,
// matches the checked-in definitions/toolchain/tool-inventory.yaml
// (semantically, not byte-for-byte - see the comment below), and is a
// pure, deterministic function of the repository's own pinning files.
func TestToolchainSupplyChainManifestRejectsUntrackedToolInput(t *testing.T) {
	t.Run("RED_entry_missing_a_required_field_is_rejected", func(t *testing.T) {
		fields := []string{"version", "source", "license", "cve_status", "owner", "update_sla", "replacement_path"}
		for _, field := range fields {
			t.Run(field, func(t *testing.T) {
				e := validEntry()
				blankField(&e, field)
				missing := e.MissingFields()
				if len(missing) != 1 || missing[0] != field {
					t.Fatalf("MissingFields() = %v, want exactly [%s]", missing, field)
				}
			})
		}
	})

	t.Run("RED_manifest_validate_rejects_an_incomplete_entry", func(t *testing.T) {
		incomplete := validEntry()
		incomplete.Name = "broken"
		incomplete.Version = ""
		m := toolinventory.Manifest{Version: 1, Tools: []toolinventory.Entry{validEntry(), incomplete}}
		if err := m.Validate(); err == nil {
			t.Fatal("expected Validate to reject a manifest containing an incomplete entry")
		}
	})

	t.Run("RED_manifest_validate_rejects_a_duplicate_tool_name", func(t *testing.T) {
		e := validEntry()
		m := toolinventory.Manifest{Version: 1, Tools: []toolinventory.Entry{e, e}}
		if err := m.Validate(); err == nil {
			t.Fatal("expected Validate to reject a manifest containing a duplicate tool name")
		}
	})

	t.Run("RED_manifest_validate_rejects_an_unrecognized_cve_status", func(t *testing.T) {
		e := validEntry()
		e.CVEStatus = "TOTALLY_FINE_TRUST_ME"
		m := toolinventory.Manifest{Version: 1, Tools: []toolinventory.Entry{e}}
		if err := m.Validate(); err == nil {
			t.Fatal("expected Validate to reject an unrecognized cve_status token")
		}
	})

	t.Run("GREEN_generated_manifest_is_complete_and_matches_the_checked_in_manifest", func(t *testing.T) {
		root := repoRoot(t)

		generated, err := toolinventory.Generate(root)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if err := generated.Validate(); err != nil {
			t.Fatalf("freshly generated manifest fails Validate: %v", err)
		}

		checkedIn, err := toolinventory.Load(filepath.Join(root, "definitions", "toolchain", "tool-inventory.yaml"))
		if err != nil {
			t.Fatalf("loading checked-in manifest: %v", err)
		}
		if err := checkedIn.Validate(); err != nil {
			t.Fatalf("checked-in manifest fails Validate: %v", err)
		}

		// Compared structurally, not byte-for-byte: `npx prettier --write
		// definitions/toolchain` reformats the checked-in YAML's
		// whitespace/quoting after it is written, and that cosmetic pass
		// must never be mistaken for supply-chain drift.
		if !toolinventory.Equal(generated, checkedIn) {
			t.Fatalf("definitions/toolchain/tool-inventory.yaml has drifted from the source files that pin it; regenerate it.\nwant %d tools: %v\ngot  %d tools: %v",
				len(generated.Tools), toolNames(generated), len(checkedIn.Tools), toolNames(checkedIn))
		}

		again, err := toolinventory.Generate(root)
		if err != nil {
			t.Fatalf("Generate (second run): %v", err)
		}
		if !toolinventory.Equal(generated, again) {
			t.Fatal("Generate is not deterministic across repeated runs")
		}
	})
}

func toolNames(m toolinventory.Manifest) []string {
	names := make([]string, len(m.Tools))
	for i, e := range m.Tools {
		names[i] = e.Name
	}
	sort.Strings(names)
	return names
}
