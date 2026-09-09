package productslice

import (
	"os"
	"path/filepath"
	"testing"
)

// goldenFixtureSlice is a small, fixed definition -- independent of the
// live tree -- so TestTodo_ALIGN_001_Golden proves the canonical JSON shape
// itself is stable rather than merely proving today's live Promotion
// values happen to match.
func goldenFixtureSlice() ProductSliceDefinition {
	return ProductSliceDefinition{
		SliceID:         "golden-fixture",
		Version:         3,
		BusinessIntents: []string{"hcmnext.people.promote_worker/v1"},
		Features:        []string{"promotion_execute"},
		Pages:           []string{"promotion.journeys.list"},
		Widgets:         []string{"widget.table.workforce@1"},
		Capabilities:    []string{"hcmnext.people.promote_worker"},
		Packages:        []string{"github.com/monstercameron/human-capital-management-suite/cmd/hcmnext"},
		Todos:           []string{"PROMO-001"},
		Jurisdictions:   []string{"US-ALL"},
		Personas:        []string{"manager"},
		ExitCriteria:    []string{"a permitted principal discovers the page"},
	}
}

// TestTodo_ALIGN_001_Golden proves the canonical serialization
// ([ProductSliceDefinition.Canonical]) that [Digest] and every downstream
// consumer of a slice's identity depends on has not silently changed shape.
// A deliberate schema change updates testdata/golden-fixture-slice.canonical.json
// together with this test, in the same review.
func TestTodo_ALIGN_001_Golden(t *testing.T) {
	got := goldenFixtureSlice().Canonical()

	goldenPath := filepath.Join("testdata", "golden-fixture-slice.canonical.json")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden fixture %s: %v", goldenPath, err)
	}

	if string(got) != string(want) {
		t.Errorf("canonical JSON drifted from %s\n got:  %s\nwant: %s", goldenPath, got, want)
	}
}

// TestTodo_ALIGN_001_Conformance is the cross-layer proof: the checked-in
// definitions/planning/product-slices.yaml is loaded exactly as any
// consumer would load it, its stored digest is verified against a fresh
// recomputation, and every reference in every admitted slice is resolved
// against a live [Registries] snapshot built from the real registries this
// contract names (feature/intent coverage, the capability BOOTSTRAP
// registry, the live Phase 1 production closure, pagedef, widgetreg and the
// todo registry). This is the one test that touches all seven registries at
// once, over the actual checked-in file rather than an in-memory fixture.
func TestTodo_ALIGN_001_Conformance(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	path := filepath.Join(root, "definitions", "planning", "product-slices.yaml")
	registryFile, err := LoadRegistryYAML(path)
	if err != nil {
		t.Fatalf("LoadRegistryYAML(%s): %v", path, err)
	}

	if err := registryFile.VerifyDigest(); err != nil {
		t.Errorf("checked-in registry digest is stale: %v", err)
	}

	if len(registryFile.Slices) == 0 {
		t.Fatal("checked-in registry has no admitted slices")
	}

	live, err := LoadLiveRegistries(root)
	if err != nil {
		t.Fatalf("LoadLiveRegistries: %v", err)
	}

	if violations := registryFile.Validate(live); !Valid(violations) {
		t.Errorf("checked-in product-slices.yaml has references that do not resolve against the live registries: %v", ViolationStrings(violations))
	}
}

// TestPromotionProductSliceRegistryNoDrift is ALIGN-001's required drift
// test: the checked-in definitions/planning/product-slices.yaml must equal
// a byte-for-byte fresh generation of the same registry
// ([PromotionSliceDefinition] wrapped by [NewRegistry] and rendered by
// [RenderRegistryFile], exactly what
// `go run ./tools/planning/cmd/productslice` writes). A hand-edit, a
// forgotten regeneration after a schema change, or a change to the
// generator's own output shape all fail this test the same way.
func TestPromotionProductSliceRegistryNoDrift(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	checkedInPath := filepath.Join(root, "definitions", "planning", "product-slices.yaml")
	checkedIn, err := os.ReadFile(checkedInPath)
	if err != nil {
		t.Fatalf("read checked-in file %s: %v", checkedInPath, err)
	}

	fresh, err := RenderRegistryFile(NewRegistry(PromotionSliceDefinition()))
	if err != nil {
		t.Fatalf("RenderRegistryFile: %v", err)
	}

	if string(checkedIn) != string(fresh) {
		t.Errorf(
			"definitions/planning/product-slices.yaml is out of date; regenerate with `go run ./tools/planning/cmd/productslice`\n--- checked-in ---\n%s\n--- fresh ---\n%s",
			checkedIn, fresh,
		)
	}
}
