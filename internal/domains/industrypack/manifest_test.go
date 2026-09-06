package industrypack

import (
	"errors"
	"testing"
)

func validPack(t *testing.T, id string, version int, ruleVersion string) IndustryPack {
	t.Helper()
	pack, err := NewIndustryPack(IndustryPack{
		PackID: id, Version: version, Industry: IndustryHealthcare,
		Owner: "hcmnext", Scope: "default", Support: "maintained",
		ParentVersion: func() int {
			if version > 1 {
				return version - 1
			}
			return 0
		}(),
		ParentDigest: func() string {
			if version > 1 {
				return "sha256:parent"
			}
			return ""
		}(),
		RulePackRefs:            []PinnedRef{{ID: "compensation-rules", Version: ruleVersion}},
		ConfigurationObjectRefs: []PinnedRef{{ID: "promotion-config", Version: "7"}},
		WorkflowDefinitionRefs:  []PinnedRef{{ID: "promotion-approval", Version: "3"}},
		ProductSliceRefs:        []PinnedRef{{ID: "promotion", Version: "1"}},
		Compatibility:           []CompatibilityDeclaration{{Component: "hcmnext", MinimumVersion: "1", MaximumVersion: "2"}},
	})
	if err != nil {
		t.Fatalf("NewIndustryPack: %v", err)
	}
	return pack
}

// TestTodo_PACK_001 proves the manifest is a typed, version-pinned contract
// with immutable digest identity and a bounded explanation surface.
func TestTodo_PACK_001(t *testing.T) {
	pack := validPack(t, "healthcare-us", 1, "4")
	if err := pack.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	digest, err := pack.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if digest == "" || digest != pack.CanonicalDigest {
		t.Fatalf("digest=%q canonical=%q", digest, pack.CanonicalDigest)
	}
	explanation, err := pack.Explain()
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if explanation.RulePackCount != 1 || explanation.CompatibilityCount != 1 {
		t.Fatalf("explanation=%+v", explanation)
	}
}

func TestTodo_PACK_001_Golden(t *testing.T) {
	left := validPack(t, "healthcare-us", 1, "4")
	right := left
	right.RulePackRefs = []PinnedRef{{ID: "compensation-rules", Version: "4"}}
	if string(left.Canonical()) != string(right.Canonical()) {
		t.Fatal("equivalent manifest values changed canonical bytes")
	}
	first, err := left.Digest()
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	second, err := right.Digest()
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if first != second {
		t.Fatalf("digest drifted: %s vs %s", first, second)
	}
}

func TestTodo_PACK_001_Conformance(t *testing.T) {
	valid := validPack(t, "healthcare-us", 1, "4")
	if err := CheckComposition(valid, valid); err != nil {
		t.Fatalf("same pin should compose: %v", err)
	}
	conflict := validPack(t, "healthcare-eu", 1, "5")
	if err := CheckComposition(valid, conflict); !errors.Is(err, ErrCompositionConflict) {
		t.Fatalf("conflict error=%v, want ErrCompositionConflict", err)
	}
	unPinned := valid
	unPinned.RulePackRefs = []PinnedRef{{ID: "compensation-rules"}}
	if err := CheckComposition(unPinned); !errors.Is(err, ErrUnpinnedReference) {
		t.Fatalf("unpinned error=%v, want ErrUnpinnedReference", err)
	}
	composition, err := Compose(valid)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(composition.References) != 4 || composition.CanonicalDigest == "" {
		t.Fatalf("composition=%+v", composition)
	}
}
