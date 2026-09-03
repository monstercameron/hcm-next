package toolinventory_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/toolinventory"
)

// TestTodo_TOOL_025_Security proves two supply-chain guards are actually
// load-bearing, not merely documentation: no entry Generate produces is
// pinned by a floating reference ("latest", "main", a bare "*"), and
// every entry carries a recognized cve_status token rather than an empty
// or ad hoc one that would be indistinguishable from "nobody checked".
func TestTodo_TOOL_025_Security(t *testing.T) {
	root := repoRoot(t)
	m, err := toolinventory.Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if toolinventory.HasFloatingVersion(m) {
		t.Error("the real generated manifest contains a floating version reference")
	}
	for _, e := range m.Tools {
		if !toolinventory.ValidCVEStatus(e.CVEStatus) {
			t.Errorf("%s: cve_status %q is not a recognized status", e.Name, e.CVEStatus)
		}
	}

	t.Run("an_injected_floating_version_is_detected", func(t *testing.T) {
		tampered := m
		tampered.Tools = append([]toolinventory.Entry{}, m.Tools...)
		tampered.Tools[0].Version = "latest"
		if !toolinventory.HasFloatingVersion(tampered) {
			t.Fatal("expected HasFloatingVersion to flag an injected \"latest\" version")
		}
	})

	t.Run("an_injected_blank_cve_status_is_rejected_by_validate", func(t *testing.T) {
		tampered := m
		tampered.Tools = append([]toolinventory.Entry{}, m.Tools...)
		tampered.Tools[0].CVEStatus = ""
		if err := tampered.Validate(); err == nil {
			t.Fatal("expected Validate to reject a blanked cve_status")
		}
	})

	t.Run("an_injected_unrecognized_cve_status_is_rejected_by_validate", func(t *testing.T) {
		tampered := m
		tampered.Tools = append([]toolinventory.Entry{}, m.Tools...)
		tampered.Tools[0].CVEStatus = "TRUST_ME"
		if err := tampered.Validate(); err == nil {
			t.Fatal("expected Validate to reject an unrecognized cve_status")
		}
	})
}
