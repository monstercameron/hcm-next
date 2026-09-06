package intentmanifests

import (
	"path/filepath"
	"testing"
)

// TestTodo_INTENT_009_RealCatalog runs the FEATURE-002 normalizer and the
// INTENT-009 classifier over the checked-in governance files rather than
// hand-built fixtures, so the universal material-feature rule is proven
// against the real intake and the real intent catalog.
func TestTodo_INTENT_009_RealCatalog(t *testing.T) {
	root := filepath.Join("..", "..", "..", "definitions", "governance")
	intents, err := LoadIntentManifestYAML(filepath.Join(root, "intent-conformance-descriptors.yaml"))
	if err != nil {
		t.Fatalf("load intent catalog: %v", err)
	}
	groups, err := LoadFeatureManifestYAML(filepath.Join(root, "feature-intent-intake.yaml"))
	if err != nil {
		t.Fatalf("load feature intake: %v", err)
	}
	if len(intents) == 0 || len(groups) == 0 {
		t.Fatalf("empty catalog: %d intents, %d groups", len(intents), len(groups))
	}
	classifier, err := NewFeatureIntentClassifier(intents)
	if err != nil {
		t.Fatalf("classifier: %v", err)
	}
	normalizer := NewFeatureNormalizer()
	features := 0
	for _, g := range groups {
		for _, f := range g.Features {
			features++
			nf := &NormalizedFeature{
				FeatureID:      f.FeatureID,
				Label:          f.Label,
				Classification: classificationFor(f.Category, f.Label),
				Domain:         g.Domain,
				IntakeLabel:    f.Label,
				IntakeGroup:    g.GroupID,
				SourceGroupID:  g.GroupID,
				SourceRef:      "definitions/governance/feature-intent-intake.yaml",
				IsMaterial:     isMaterialOperation(f.Label),
			}
			if err := normalizer.AddNormalizedFeature(nf); err != nil {
				t.Fatalf("group %d feature %s: %v", g.GroupID, f.FeatureID, err)
			}
			if err := normalizer.VerifySourceTraceable(nf); err != nil {
				t.Fatalf("feature %s lost its intake: %v", f.FeatureID, err)
			}
			fc, err := classifier.ClassifyFeature(nf, f.MappedIntentID)
			if err != nil {
				t.Fatalf("feature %s: %v", f.FeatureID, err)
			}
			classifier.StoreClassification(fc)
			if f.MappedIntentID != "" && f.MappedIntentID != DeferredIntentBinding && fc.IsMaterial {
				if err := classifier.ValidateMaterialFeatureHasBoundIntent(fc); err != nil {
					t.Fatalf("feature %s: %v", f.FeatureID, err)
				}
			}
		}
	}
	if err := normalizer.ValidateNormalization(); err != nil {
		t.Fatalf("normalization over the real intake: %v", err)
	}
	if invalid := classifier.ReportsInvalidBindings(); len(invalid) != 0 {
		t.Fatalf("real intake binds material features to non-catalog intents: %v", invalid)
	}
	t.Logf("real intake: %d groups, %d features, %d intents, %d material features unbound (review queue)",
		len(groups), features, len(intents), len(classifier.ReportsUnboundMaterialFeatures()))
}

func classificationFor(category, label string) SemanticClassification {
	if isNonMaterialOperation(label) && !isMaterialOperation(label) {
		return ClassNonMaterial
	}
	switch category {
	case "CREATE", "CHANGE", "CALCULATE":
		return ClassCreate
	case "OBSERVE":
		return ClassObserve
	default:
		return ClassReviewNeeded
	}
}
