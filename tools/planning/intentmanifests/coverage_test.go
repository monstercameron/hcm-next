package intentmanifests

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFeatureIntentCoverageRejectsImplicitFeature(t *testing.T) {
	registry := FeatureIntentCoverageRegistry{
		Version:           "1.0",
		FeatureGroups:     1,
		FeatureCount:      1,
		DispositionCounts: map[FeatureDisposition]int{DispositionReview: 1},
		Features: []FeatureIntentCoverage{{
			FeatureID:            "feature_without_owner",
			Group:                1,
			GroupName:            "People",
			CanonicalIdentity:    "hcmnext.people.feature_without_owner",
			Classification:       ClassCreate,
			Role:                 RoleIntentCreator,
			CoverageStatus:       CoverageDeferred,
			BoundIntentID:        DeferredIntentBinding,
			Disposition:          DispositionReview,
			MappingIssue:         "MISSING_BINDING",
			DispositionRationale: "requires review",
			Owner:                "people",
			Phase:                "DESIGN",
			Depth:                "INTAKE",
			Actor:                "initiator",
			Channel:              "governed",
			Subject:              "people",
			Resource:             "worker",
			Capability:           "intent:hcmnext.people.feature_without_owner",
			CapabilityVersion:    "1",
			InputSchema:          "properties:worker",
			ResultSchema:         "result:worker",
			ParentChildBehavior:  "independent-until-cataloged",
			GovernanceProfile:    "owner:people",
			EvidenceExpectation:  "source_provenance",
		}},
	}
	if err := ValidateFeatureIntentCoverage(registry); err != nil {
		t.Fatalf("complete row rejected: %v", err)
	}
	registry.Features[0].Owner = ""
	if err := ValidateFeatureIntentCoverage(registry); err == nil {
		t.Fatal("expected implicit feature dimension to be rejected")
	}
}

func TestFeatureExtensionDispositionRejectsSilentAliasOrScopeExpansion(t *testing.T) {
	groups := []FeatureGroup{{
		GroupID: 1, Name: "People", Domain: "people",
		Features: []Feature{
			{FeatureID: "worker_explain", Label: "Explain Worker State", Category: "OBSERVE", MappedIntentID: "hcmnext.people.explain_worker_state/v1"},
			{FeatureID: "worker_create", Label: "Create Worker", Category: "CREATE", MappedIntentID: "DEFERRED"},
			{FeatureID: "render_worker", Label: "Render Worker Card", Category: "OBSERVE", MappedIntentID: "DEFERRED"},
			{FeatureID: "approval_decision", Label: "Record Approval Decision", Category: "CHANGE", MappedIntentID: "hcmnext.people.explain_worker_state/v1,hcmnext.people.explain_worker_state/v1"},
		},
	}}
	registry, err := BuildFeatureIntentCoverage(groups, []IntentDescriptor{{IntentTypeID: "hcmnext.people.explain_worker_state", Version: 1, DisplayName: "ExplainWorkerState", Phase: "GATE_A"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFeatureIntentCoverage(registry); err != nil {
		t.Fatal(err)
	}
	for _, record := range registry.Features {
		switch record.FeatureID {
		case "worker_explain":
			if record.Disposition != DispositionMergedInto || record.DispositionTarget == "" {
				t.Errorf("bound feature silently expanded: %+v", record)
			}
		case "worker_create":
			if record.Disposition != DispositionDeferredToIntent || record.DispositionTarget == "" {
				t.Errorf("deferred feature lacks future intent disposition: %+v", record)
			}
		case "render_worker":
			if record.Disposition != DispositionNonMaterial {
				t.Errorf("mechanic was not classified NON_MATERIAL: %+v", record)
			}
		case "approval_decision":
			if record.Disposition != DispositionReview || record.MappingIssue == "" {
				t.Errorf("ambiguous binding escaped review: %+v", record)
			}
		}
	}
}

func TestTodo_FEATURE_003_Golden(t *testing.T) {
	registry := minimalCoverageRegistry()
	if err := ValidateFeatureIntentCoverage(registry); err != nil {
		t.Fatal(err)
	}
	if registry.DispositionCounts[DispositionReview] != 1 {
		t.Fatalf("golden review count = %d, want 1", registry.DispositionCounts[DispositionReview])
	}
}

func TestTodo_INTENT_010_Golden(t *testing.T) {
	groups := []FeatureGroup{{GroupID: 1, Name: "People", Domain: "people", Features: []Feature{{FeatureID: "worker_create", Label: "Create Worker", Category: "CREATE", MappedIntentID: "DEFERRED"}}}}
	intents := []IntentDescriptor{{IntentTypeID: "hcmnext.people.explain_worker_state", Version: 1, DisplayName: "ExplainWorkerState"}}
	first, err := BuildFeatureIntentCoverage(groups, intents)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildFeatureIntentCoverage(groups, intents)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || first.Digest != second.Digest {
		t.Fatalf("digest is not stable: %q vs %q", first.Digest, second.Digest)
	}
	if first.Features[0].Disposition != DispositionDeferredToIntent || first.Features[0].DispositionTarget != "hcmnext.people.worker_create/v1" {
		t.Fatalf("unexpected deferred disposition: %+v", first.Features[0])
	}
}

func FuzzTodo_INTENT_010(f *testing.F) {
	f.Add("worker_create", "people")
	f.Add("review", "operations")
	f.Fuzz(func(t *testing.T, featureID, domain string) {
		if featureID == "" || domain == "" {
			t.Skip()
		}
		groups := []FeatureGroup{{GroupID: 1, Name: "Group", Domain: domain, Features: []Feature{{FeatureID: featureID, Label: "Create Feature", Category: "CREATE", MappedIntentID: "DEFERRED"}}}}
		registry, err := BuildFeatureIntentCoverage(groups, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateFeatureIntentCoverage(registry); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTodo_INTENT_010_Fault(t *testing.T) {
	groups := []FeatureGroup{{GroupID: 1, Name: "People", Domain: "people", Features: []Feature{{FeatureID: "worker_create", Label: "Create Worker", Category: "CREATE", MappedIntentID: "DEFERRED"}, {FeatureID: "worker_create", Label: "Duplicate", Category: "CREATE", MappedIntentID: "DEFERRED"}}}}
	if _, err := BuildFeatureIntentCoverage(groups, nil); err == nil {
		t.Fatal("expected duplicate feature id to fail the build")
	}
}

func TestTodo_INTENT_010_Security(t *testing.T) {
	registry := minimalCoverageRegistry()
	registry.Features[0].Disposition = FeatureDisposition("EXECUTE_WITHOUT_REVIEW")
	if err := ValidateFeatureIntentCoverage(registry); err == nil {
		t.Fatal("expected unknown disposition to be rejected")
	}
}

func TestTodo_INTENT_010_Mutation(t *testing.T) {
	registry := minimalCoverageRegistry()
	registry.DispositionCounts[DispositionReview] = 2
	if err := ValidateFeatureIntentCoverage(registry); err == nil {
		t.Fatal("expected mutated disposition count to be rejected")
	}
}

func TestFeatureIntentCoverageGeneratedFileHasNoDrift(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	intents, err := LoadIntentManifestYAML(filepath.Join(root, "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := LoadFeatureManifestYAML(filepath.Join(root, "definitions", "governance", "feature-intent-intake.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := BuildFeatureIntentCoverage(groups, intents)
	if err != nil {
		t.Fatal(err)
	}
	want, err := MarshalFeatureIntentCoverageYAML(registry)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "definitions", "governance", "feature-intent-coverage.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("generated feature-intent-coverage.yaml is out of date")
	}
}

func minimalCoverageRegistry() FeatureIntentCoverageRegistry {
	return FeatureIntentCoverageRegistry{
		Version:           "1.0",
		FeatureGroups:     1,
		FeatureCount:      1,
		DispositionCounts: map[FeatureDisposition]int{DispositionReview: 1},
		Features: []FeatureIntentCoverage{{
			FeatureID: "feature", Group: 1, GroupName: "Group", CanonicalIdentity: "hcmnext.group.feature",
			Classification: ClassCreate, Role: RoleIntentCreator, CoverageStatus: CoverageDeferred,
			BoundIntentID: DeferredIntentBinding, Disposition: DispositionReview, MappingIssue: "MISSING_BINDING",
			DispositionRationale: "requires review", Owner: "group", Phase: "DESIGN", Depth: "INTAKE",
			Actor: "initiator", Channel: "governed", Subject: "group", Resource: "feature",
			Capability: "intent:hcmnext.group.feature", CapabilityVersion: "1", InputSchema: "properties:feature",
			ResultSchema: "result:feature", ParentChildBehavior: "independent-until-cataloged",
			GovernanceProfile: "owner:group", EvidenceExpectation: "source_provenance",
		}},
	}
}
