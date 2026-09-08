package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-076: semantic-region composition validation. Drafts name
// regions nothing checks against the page-anatomy contract: an unknown
// region, a platform-owned shell claimed as composable content, regions
// out of anatomy order, a duplicated primary, or a composition with no
// primary region at all would all validate today. The lifecycle's next
// validate step needs a pure region verdict — registered regions in
// anatomy order with exactly one primary — with stable reasons.
func TestTodo_WEB_076(t *testing.T) {
	full := []string{"identity", "authority", "navigation", "primary", "supporting", "utility", "completion"}
	for _, composition := range []struct {
		name     string
		regions  []string
		accepted bool
		reasons  []string
	}{
		{"full anatomy order validates", full, true, nil},
		{"empty composition validates", nil, true, nil},
		{"single primary validates", []string{"primary"}, true, nil},
		{"repeated supporting in order validates", []string{"primary", "supporting", "supporting"}, true, nil},
		{"reversed regions refuse", []string{"completion", "primary"}, false, []string{`region "primary" out of order`}},
		{"unknown region refuses", []string{"primary", "lobby"}, false, []string{`unknown region "lobby"`}},
		{"platform shell refuses", []string{"shell", "primary"}, false, []string{`platform-owned region "shell"`}},
		{"duplicate primary refuses", []string{"primary", "supporting", "primary"}, false, []string{`region "primary" out of order`, `duplicate primary region`}},
		{"composition without primary refuses", []string{"supporting", "utility"}, false, []string{`missing primary region`}},
	} {
		verdict := ValidateRegionComposition(PageComposition{Regions: composition.regions})
		if verdict.Compatible != composition.accepted || !reflect.DeepEqual(verdict.Reasons, composition.reasons) {
			t.Fatalf("%s = (%t, %q), want (%t, %q)", composition.name, verdict.Compatible, verdict.Reasons, composition.accepted, composition.reasons)
		}
	}

	// A draft validates through its regions alongside its floorplan.
	draft := PageDraft{Page: "studio", Composition: PageComposition{
		Floorplan: "composition-studio", FloorplanVersion: 1, Primitives: []string{"stack"}, Regions: full,
	}}
	if verdict := ValidateDraftRegions(draft); !verdict.Compatible {
		t.Fatalf("anatomy draft fails region validation: %q", verdict.Reasons)
	}
	if verdict := ValidateDraftFloorplan(draft, RegisteredFloorplans()); !verdict.Compatible {
		t.Fatalf("anatomy draft fails floorplan validation: %q", verdict.Reasons)
	}
	broken := draft
	broken.Composition.Regions = []string{"completion", "primary"}
	if verdict := ValidateDraftRegions(broken); verdict.Compatible {
		t.Fatal("disordered draft passes region validation")
	}
}

// Golden: region validation outcomes over a composition matrix.
func TestTodo_WEB_076_Golden(t *testing.T) {
	compositions := [][]string{
		{"identity", "primary", "supporting"},
		{"supporting", "primary"},
		{"primary", "lobby"},
		{"shell"},
		{},
	}
	var builder strings.Builder
	for _, regions := range compositions {
		verdict := ValidateRegionComposition(PageComposition{Regions: regions})
		builder.WriteString(strings.Join(regions, ","))
		builder.WriteString("\x00")
		if verdict.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(verdict.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "f6227695fbcdd2179dffff9ad90d547791254526232b575900053d0aba65efb0"
	if got != want {
		t.Fatalf("region matrix digest = %s, want %s", got, want)
	}
}

// Browser: every catalog floorplan carries the full anatomy
// composition compatibly on both layers — deterministically.
func TestTodo_WEB_076_Browser(t *testing.T) {
	catalog := RegisteredFloorplans()
	full := []string{"identity", "authority", "navigation", "primary", "supporting", "utility", "completion"}
	for _, floorplan := range catalog.Floorplans {
		draft := PageDraft{Page: PageID(floorplan.ID), Composition: PageComposition{
			Floorplan: floorplan.ID, FloorplanVersion: floorplan.Version,
			Primitives: RegisteredPrimitives(), Regions: full,
		}}
		floorplanVerdict := ValidateDraftFloorplan(draft, catalog)
		regionVerdict := ValidateDraftRegions(draft)
		if !floorplanVerdict.Compatible || !regionVerdict.Compatible {
			t.Fatalf("floorplan %q rejects the anatomy draft: %q %q", floorplan.ID, floorplanVerdict.Reasons, regionVerdict.Reasons)
		}
		again := ValidateDraftRegions(draft)
		if !reflect.DeepEqual(regionVerdict, again) {
			t.Fatalf("floorplan %q region validation is nondeterministic", floorplan.ID)
		}
	}
}

// Conformance: catalog determinism, empty composition, reason
// stability.
func TestTodo_WEB_076_Conformance(t *testing.T) {
	if !reflect.DeepEqual(RegisteredRegions(), RegisteredRegions()) {
		t.Fatal("region catalog is nondeterministic")
	}
	if verdict := ValidateRegionComposition(PageComposition{}); !verdict.Compatible {
		t.Fatalf("empty composition fails: %q", verdict.Reasons)
	}
	first := ValidateRegionComposition(PageComposition{Regions: []string{"completion", "lobby"}})
	second := ValidateRegionComposition(PageComposition{Regions: []string{"completion", "lobby"}})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("region reasons are unstable")
	}
}
