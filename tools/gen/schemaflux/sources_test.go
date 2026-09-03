package schemaflux_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sfx "github.com/monstercameron/hcm-next/tools/gen/schemaflux"
)

// TestTodo_MSRC_001 is the MSRC-001 primary test: it builds the definition
// source manifest for exactly the fourteen definitions and the P1A
// capability manifest entries, writes it to
// definitions/generation/definition-sources.yaml, and asserts the exact
// counts and that every entry carries a source file, stable ID, owner and
// (for capability entries) a storage disposition.
func TestTodo_MSRC_001(t *testing.T) {
	repoRoot := findRepoRoot(t)
	catalog := loadValidCatalog(t)

	manifest := sfx.BuildDefinitionSourceManifest(catalog, func(absolute string) string {
		rel, err := filepath.Rel(repoRoot, absolute)
		if err != nil {
			t.Fatalf("relativize %s to %s: %v", absolute, repoRoot, err)
		}
		return rel
	})

	if len(manifest.Definitions) != 14 {
		t.Fatalf("manifest enumerates %d definitions, want exactly 14", len(manifest.Definitions))
	}
	if len(manifest.CapabilityManifest) != 10 {
		t.Fatalf("manifest enumerates %d capability manifest entries, want exactly 10", len(manifest.CapabilityManifest))
	}

	for _, d := range manifest.Definitions {
		if d.SourceFile == "" {
			t.Errorf("%s: missing source_file", d.IntentTypeID)
		}
		if d.OwnerPlane == "" || d.OwnerDomain == "" {
			t.Errorf("%s: missing owner_plane/owner_domain", d.IntentTypeID)
		}
		if d.Digest == "" {
			t.Errorf("%s: missing digest", d.IntentTypeID)
		}
		if d.Version == 0 {
			t.Errorf("%s: missing version", d.IntentTypeID)
		}
	}
	for _, c := range manifest.CapabilityManifest {
		if c.SourceFile == "" {
			t.Errorf("%s: missing source_file", c.ID)
		}
		if c.OwnerDomain == "" {
			t.Errorf("%s: missing owner_domain", c.ID)
		}
		if c.StorageDisposition == "" {
			t.Errorf("%s: missing storage_disposition", c.ID)
		}
		if c.Digest == "" {
			t.Errorf("%s: missing digest", c.ID)
		}
	}

	out := filepath.Join(repoRoot, "definitions", "generation", "definition-sources.yaml")
	if err := sfx.WriteFile(out, manifest.YAML()); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read back %s: %v", out, err)
	}
	if !strings.Contains(string(written), "definitions: 14") {
		t.Error("written manifest does not report definitions: 14")
	}
	if !strings.Contains(string(written), "capability_manifests: 10") {
		t.Error("written manifest does not report capability_manifests: 10")
	}
}

// TestTodo_MSRC_001_Golden pins the manifest's structural shape: the sorted
// list of every intent_type_id and capability id it must enumerate, so an
// accidental addition, removal or rename in either source is caught here
// rather than only by a downstream consumer of the manifest.
func TestTodo_MSRC_001_Golden(t *testing.T) {
	catalog := loadValidCatalog(t)

	var gotIDs []string
	for _, d := range catalog.Definitions {
		gotIDs = append(gotIDs, d.IntentTypeID)
	}
	wantIDs := []string{
		"hcmnext.intelligence.explain_transaction",
		"hcmnext.operations.create_repair_plan",
		"hcmnext.operations.detect_drift",
		"hcmnext.operations.simulate_repair",
		"hcmnext.people.change_manager",
		"hcmnext.people.explain_worker_state",
		"hcmnext.people.promote_worker",
		"hcmnext.rewards.change_base_pay",
		"hcmnext.rewards.evaluate_pay_band_position",
		"hcmnext.rewards.release_compensation_budget",
		"hcmnext.rewards.reserve_compensation_budget",
		"hcmnext.rewards.simulate_compensation",
		"hcmnext.work.approve_proposal",
		"hcmnext.work.reject_proposal",
	}
	assertStringSlicesEqual(t, "definition intent_type_id", gotIDs, wantIDs)

	var gotCapIDs []string
	for _, c := range catalog.CapabilityManifest {
		gotCapIDs = append(gotCapIDs, c.ID)
	}
	wantCapIDs := []string{
		"hcmnext.intelligence.explain_transaction",
		"hcmnext.operations.create_repair_plan",
		"hcmnext.operations.detect_drift",
		"hcmnext.operations.simulate_repair",
		"hcmnext.people.explain_worker_state",
		"hcmnext.people.promote_worker",
		"hcmnext.registry.explain_capability",
		"hcmnext.registry.resolve_capability",
		"hcmnext.rewards.evaluate_pay_band_position",
		"hcmnext.rewards.simulate_compensation",
	}
	assertStringSlicesEqual(t, "capability manifest id", gotCapIDs, wantCapIDs)
}

func assertStringSlicesEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d entries %v, want %d entries %v", label, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
