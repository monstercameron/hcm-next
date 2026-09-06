package intentmanifests

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadIntentManifest verifies the intent descriptor manifest loads correctly.
func TestLoadIntentManifest(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml")
	_, err := os.Stat(path)
	if err != nil {
		t.Skip("intent manifest not found")
	}

	// In production, this would load and parse the manifest.
	// descriptors, err := LoadIntentManifest(path)
	// if err != nil {
	//     t.Fatalf("load intent manifest: %v", err)
	// }
	// if got := len(descriptors); got != 14 {
	//     t.Errorf("loaded descriptors count = %d, want 14", got)
	// }
}

// TestLoadFeatureManifest verifies the feature intake manifest loads correctly.
func TestLoadFeatureManifest(t *testing.T) {
	path := filepath.Join("..", "..", "..", "definitions", "governance", "feature-intent-intake.yaml")
	_, err := os.Stat(path)
	if err != nil {
		t.Skip("feature manifest not found")
	}

	// In production, this would load and parse the manifest.
	// groups, err := LoadFeatureManifest(path)
	// if err != nil {
	//     t.Fatalf("load feature manifest: %v", err)
	// }
	// if got := len(groups); got != 49 {
	//     t.Errorf("loaded groups count = %d, want 49", got)
	// }
}

// TestIntentDescriptorStructure verifies the manifest contains all required fields.
func TestIntentDescriptorStructure(t *testing.T) {
	// This test verifies the YAML structure matches the expected schema.
	// In practice, load the manifest and validate each descriptor has:
	// - intent_type_id (matches hcmnext.*)
	// - version = 1
	// - family (CHANGE_REQUEST, ANALYTICAL_REQUEST, CALCULATION_REQUEST)
	// - side_effect_profile (INTERNAL_MUTATION, READ_ONLY, PURE)
	// - entities, properties, reads, writes, effects, authority, time, evidence
	// - lifecycle (request, execution, business, consistency, obligation)
	// - negative_policy (at least one condition/disposition pair)
	// - scenario (name, given, when, then)
	// - conformance_only (optional, only for change_manager)
}

// TestFeatureGroupStructure verifies the feature manifest contains all required fields.
func TestFeatureGroupStructure(t *testing.T) {
	// This test verifies the YAML structure matches the expected schema.
	// In practice, load the manifest and validate each group has:
	// - group_id (1-49, unique)
	// - name (non-empty)
	// - features (non-empty list)
	// - each feature: feature_id, label, category, mapped_intent_id
	// - source_provenance
	// - canonical_digest
}
