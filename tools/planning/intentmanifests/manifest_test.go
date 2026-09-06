package intentmanifests

import (
	"path/filepath"
	"testing"
)

// TestLoadAndValidateIntentManifest loads and validates the intent manifest from YAML.
func TestLoadAndValidateIntentManifest(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml")

	// Load the manifest.
	descriptors, err := LoadIntentManifestYAML(path)
	if err != nil {
		t.Fatalf("load intent manifest: %v", err)
	}

	// Validate the manifest.
	if err := ValidateIntentManifestYAML(descriptors); err != nil {
		t.Fatalf("validate intent manifest: %v", err)
	}

	// Verify count.
	if got := len(descriptors); got != 14 {
		t.Errorf("intent descriptors count = %d, want 14", got)
	}

	// Verify change_manager is conformance-only.
	for _, d := range descriptors {
		if d.IntentTypeID == "hcmnext.people.change_manager" && !d.ConformanceOnly {
			t.Error("change_manager must be marked conformance_only")
		}
	}

	// Compute digest and verify it has expected length.
	digest, err := ComputeIntentDigestYAML(descriptors)
	if err != nil {
		t.Fatalf("compute digest: %v", err)
	}
	if len(digest) != 64 {
		t.Errorf("digest length = %d, want 64", len(digest))
	}

	t.Logf("Intent manifest valid; descriptor digest: %s", digest)
}

// TestLoadAndValidateFeatureManifest loads and validates the feature manifest from YAML.
func TestLoadAndValidateFeatureManifest(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "feature-intent-intake.yaml")

	// Load the manifest.
	groups, err := LoadFeatureManifestYAML(path)
	if err != nil {
		t.Fatalf("load feature manifest: %v", err)
	}

	// Validate the manifest.
	if err := ValidateFeatureManifestYAML(groups); err != nil {
		t.Fatalf("validate feature manifest: %v", err)
	}

	// Verify count.
	if got := len(groups); got != 49 {
		t.Errorf("feature groups count = %d, want 49", got)
	}

	// Verify all group IDs 1-49 are present.
	seenIDs := make(map[int]bool)
	for _, g := range groups {
		seenIDs[g.GroupID] = true
		if len(g.Features) == 0 {
			t.Errorf("group %d: no features", g.GroupID)
		}
	}
	for i := 1; i <= 49; i++ {
		if !seenIDs[i] {
			t.Errorf("missing group_id %d", i)
		}
	}

	// Compute digest and verify it has expected length.
	digest, err := ComputeFeatureDigestYAML(groups)
	if err != nil {
		t.Fatalf("compute digest: %v", err)
	}
	if len(digest) != 64 {
		t.Errorf("digest length = %d, want 64", len(digest))
	}

	t.Logf("Feature manifest valid; group digest: %s", digest)
}

// TestIntentManifestDeterminism verifies the intent manifest digest is deterministic.
func TestIntentManifestDeterminism(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml")

	// Load twice and compute digests.
	d1, err := LoadIntentManifestYAML(path)
	if err != nil {
		t.Fatalf("load (1): %v", err)
	}
	d2, err := LoadIntentManifestYAML(path)
	if err != nil {
		t.Fatalf("load (2): %v", err)
	}

	dig1, err := ComputeIntentDigestYAML(d1)
	if err != nil {
		t.Fatal(err)
	}
	dig2, err := ComputeIntentDigestYAML(d2)
	if err != nil {
		t.Fatal(err)
	}

	if dig1 != dig2 {
		t.Errorf("intent digest not deterministic: %q vs %q", dig1, dig2)
	}
}

// TestFeatureManifestDeterminism verifies the feature manifest digest is deterministic.
func TestFeatureManifestDeterminism(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "feature-intent-intake.yaml")

	// Load twice and compute digests.
	g1, err := LoadFeatureManifestYAML(path)
	if err != nil {
		t.Fatalf("load (1): %v", err)
	}
	g2, err := LoadFeatureManifestYAML(path)
	if err != nil {
		t.Fatalf("load (2): %v", err)
	}

	dig1, err := ComputeFeatureDigestYAML(g1)
	if err != nil {
		t.Fatal(err)
	}
	dig2, err := ComputeFeatureDigestYAML(g2)
	if err != nil {
		t.Fatal(err)
	}

	if dig1 != dig2 {
		t.Errorf("feature digest not deterministic: %q vs %q", dig1, dig2)
	}
}

// TestIntentDescriptorFields verifies all required fields are present.
func TestIntentDescriptorFields(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml")

	descriptors, err := LoadIntentManifestYAML(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, d := range descriptors {
		if d.IntentTypeID == "" {
			t.Error("missing intent_type_id")
		}
		if d.DisplayName == "" {
			t.Error("missing display_name")
		}
		if d.Family == "" {
			t.Error("missing family")
		}
		if d.SideEffectProfile == "" {
			t.Error("missing side_effect_profile")
		}
		if len(d.Entities) == 0 {
			t.Error("missing entities")
		}
		if len(d.Properties) == 0 {
			t.Error("missing properties")
		}
		if len(d.Reads) == 0 {
			t.Error("missing reads")
		}
		if len(d.Writes) == 0 {
			t.Error("missing writes")
		}
		if len(d.Effects) == 0 {
			t.Error("missing effects")
		}
		if len(d.Authority) == 0 {
			t.Error("missing authority")
		}
		if len(d.Time) == 0 {
			t.Error("missing time")
		}
		if len(d.Evidence) == 0 {
			t.Error("missing evidence")
		}
		if len(d.NegativePolicy) == 0 {
			t.Errorf("%s: missing negative_policy", d.IntentTypeID)
		}
		if d.Scenario.Name == "" {
			t.Errorf("%s: missing scenario.name", d.IntentTypeID)
		}
		if d.Scenario.Given == "" {
			t.Errorf("%s: missing scenario.given", d.IntentTypeID)
		}
		if d.Scenario.When == "" {
			t.Errorf("%s: missing scenario.when", d.IntentTypeID)
		}
		if d.Scenario.Then == "" {
			t.Errorf("%s: missing scenario.then", d.IntentTypeID)
		}
	}
}

// TestFeatureGroupFields verifies all required fields are present.
func TestFeatureGroupFields(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "feature-intent-intake.yaml")

	groups, err := LoadFeatureManifestYAML(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, g := range groups {
		if g.GroupID < 1 || g.GroupID > 49 {
			t.Errorf("invalid group_id: %d", g.GroupID)
		}
		if g.Name == "" {
			t.Errorf("group %d: missing name", g.GroupID)
		}
		if len(g.Features) == 0 {
			t.Errorf("group %d: no features", g.GroupID)
		}
		if g.SourceProvenance == "" {
			t.Errorf("group %d: missing source_provenance", g.GroupID)
		}
		if g.CanonicalDigest == "" {
			t.Errorf("group %d: missing canonical_digest", g.GroupID)
		}

		for _, f := range g.Features {
			if f.FeatureID == "" {
				t.Errorf("group %d: missing feature_id", g.GroupID)
			}
			if f.Label == "" {
				t.Errorf("group %d: missing feature label", g.GroupID)
			}
			if f.Category == "" {
				t.Errorf("group %d: missing feature category", g.GroupID)
			}
			if f.MappedIntentID == "" {
				t.Errorf("group %d: missing mapped_intent_id", g.GroupID)
			}
		}
	}
}
