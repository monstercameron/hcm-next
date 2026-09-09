package pagedef_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/ssrshell"
)

func TestTodo_WEB_004(t *testing.T) {
	vocabulary := pagedef.SemanticRegionVocabulary()
	if err := vocabulary.Validate(); err != nil {
		t.Fatalf("semantic-region vocabulary: %v", err)
	}
	if got, want := len(vocabulary.Regions), len(pagedef.RegionKinds()); got != want {
		t.Fatalf("region count = %d, want %d", got, want)
	}
	if vocabulary.Digest() != pagedef.SemanticRegionVocabularyDigest() {
		t.Fatal("canonical vocabulary digest is not stable")
	}
	for _, kind := range pagedef.RegionKinds() {
		region, ok := vocabulary.Lookup(kind)
		if !ok {
			t.Fatalf("vocabulary has no entry for %q", kind)
		}
		if region.LandmarkRole == "" || len(region.RequiredAccessibilityAttributes) == 0 {
			t.Fatalf("%q is missing landmark/accessibility semantics: %+v", kind, region)
		}
	}
}

func TestTodo_WEB_004_Golden(t *testing.T) {
	const want = "sha256:87d8b29f4413965e7237b4da6a0d281b59a25bc2ead2eabb71562e00937da3f2"
	if got := pagedef.SemanticRegionVocabularyDigest(); got != want {
		t.Fatalf("semantic-region vocabulary digest = %q, want %q", got, want)
	}
}

func TestTodo_WEB_004_Browser(t *testing.T) {
	// The vocabulary is a renderer-independent Go contract. Its browser-facing
	// guarantee is represented by the landmark and accessibility declarations,
	// which the conformance test checks against ssrshell's read-only map.
	vocabulary := pagedef.SemanticRegionVocabulary()
	for _, region := range vocabulary.Regions {
		if region.MayHostLiveRegion && len(region.RequiredAccessibilityAttributes) == 0 {
			t.Fatalf("live-region host %q has no accessibility requirements", region.Kind)
		}
	}
}

func TestTodo_WEB_004_Conformance(t *testing.T) {
	vocabulary := pagedef.SemanticRegionVocabulary()
	for _, kind := range pagedef.RegionKinds() {
		if _, ok := vocabulary.Lookup(kind); !ok {
			t.Fatalf("vocabulary omits %q", kind)
		}
		tag, label, ok := ssrshell.LandmarkForRegionKind(kind)
		if !ok {
			t.Fatalf("ssrshell landmark map omits %q", kind)
		}
		if label == "" || !contains(ssrshell.LandmarkTags, tag) {
			t.Fatalf("ssrshell mapping for %q = tag %q, label %q; want a declared landmark tag and label", kind, tag, label)
		}
	}
	mutated := vocabulary
	mutated.Regions = append([]pagedef.SemanticRegion(nil), vocabulary.Regions...)
	mutated.Regions[0].AllowedChildRegionKinds = append(mutated.Regions[0].AllowedChildRegionKinds, pagedef.RegionKind("unknown"))
	if err := mutated.Validate(); err == nil {
		t.Fatal("vocabulary accepted an unknown child region kind")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
