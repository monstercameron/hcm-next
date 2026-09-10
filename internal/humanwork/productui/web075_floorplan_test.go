package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-075: floorplan compatibility validation. Drafts carry
// compositions, but nothing validates them against the registered
// floorplan contracts: an unknown floorplan, a future floorplan
// version, or an unregistered primitive would sail through validate
// into publish. The lifecycle's validate step needs a pure
// compatibility verdict over the spec floorplan catalog — known
// floorplan, supported version, registered primitives — with stable
// reasons, before any draft may publish.
func TestTodo_WEB_075(t *testing.T) {
	catalog := RegisteredFloorplans()
	if len(catalog.Floorplans) != 10 {
		t.Fatalf("floorplan catalog registers %d floorplans, want 10", len(catalog.Floorplans))
	}
	compatible := PageComposition{Floorplan: "collection", FloorplanVersion: 1, Primitives: []string{"stack", "table"}}
	for _, validation := range []struct {
		name        string
		composition PageComposition
		compatible  bool
		reasons     []string
	}{
		{"registered floorplan validates", compatible, true, nil},
		{"blank floorplan refuses", PageComposition{FloorplanVersion: 1}, false, []string{`unknown floorplan ""`}},
		{"unknown floorplan refuses", PageComposition{Floorplan: "maze", FloorplanVersion: 1}, false, []string{`unknown floorplan "maze"`}},
		{"zero version refuses", PageComposition{Floorplan: "collection"}, false, []string{`unsupported floorplan version 0 for "collection"`}},
		{"future version refuses", PageComposition{Floorplan: "collection", FloorplanVersion: 2}, false, []string{`unsupported floorplan version 2 for "collection"`}},
		{"unknown primitive refuses", PageComposition{Floorplan: "collection", FloorplanVersion: 1, Primitives: []string{"stack", "marquee"}}, false, []string{`unknown primitive "marquee"`}},
		{"empty primitives validate", PageComposition{Floorplan: "object", FloorplanVersion: 1}, true, nil},
	} {
		verdict := ValidateFloorplanCompatibility(validation.composition, catalog)
		if verdict.Compatible != validation.compatible || !reflect.DeepEqual(verdict.Reasons, validation.reasons) {
			t.Fatalf("%s = (%t, %q), want (%t, %q)", validation.name, verdict.Compatible, verdict.Reasons, validation.compatible, validation.reasons)
		}
	}

	// A draft validates through its composition.
	draft := PageDraft{Page: "studio", Snapshot: PageDefinitionSnapshot{Page: "studio"}, Composition: compatible}
	if verdict := ValidateDraftFloorplan(draft, catalog); !verdict.Compatible {
		t.Fatalf("compatible draft fails validation: %q", verdict.Reasons)
	}
	broken := draft
	broken.Composition.Floorplan = "maze"
	if verdict := ValidateDraftFloorplan(broken, catalog); verdict.Compatible {
		t.Fatal("incompatible draft passes validation")
	}
}

// Golden: validation outcomes over a composition matrix.
func TestTodo_WEB_075_Golden(t *testing.T) {
	catalog := RegisteredFloorplans()
	compositions := []PageComposition{
		{Floorplan: "collection", FloorplanVersion: 1, Primitives: []string{"stack", "table"}},
		{Floorplan: "maze", FloorplanVersion: 1},
		{Floorplan: "object", FloorplanVersion: 3, Primitives: []string{"marquee"}},
		{Floorplan: "analysis", FloorplanVersion: 1},
	}
	var builder strings.Builder
	for _, composition := range compositions {
		verdict := ValidateFloorplanCompatibility(composition, catalog)
		builder.WriteString(composition.Floorplan)
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
	const want = "73a56360aa69e50cb03c54711ebd39ae38aab8823059057b7e94423b3b9c9207"
	if got != want {
		t.Fatalf("floorplan matrix digest = %s, want %s", got, want)
	}
}

// Browser: every catalog floorplan validates a canonical draft using
// the full registered primitive set — deterministically.
func TestTodo_WEB_075_Browser(t *testing.T) {
	catalog := RegisteredFloorplans()
	primitives := make([]string, 0)
	primitives = append(primitives, RegisteredPrimitives()...)
	for _, floorplan := range catalog.Floorplans {
		composition := PageComposition{Floorplan: floorplan.ID, FloorplanVersion: floorplan.Version, Primitives: primitives}
		first := ValidateFloorplanCompatibility(composition, catalog)
		second := ValidateFloorplanCompatibility(composition, catalog)
		if !first.Compatible {
			t.Fatalf("floorplan %q rejects the canonical draft: %q", floorplan.ID, first.Reasons)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("floorplan %q validation is nondeterministic", floorplan.ID)
		}
	}
}

// Conformance: catalog determinism, empty-catalog refusal, reason
// stability.
func TestTodo_WEB_075_Conformance(t *testing.T) {
	if !reflect.DeepEqual(RegisteredFloorplans(), RegisteredFloorplans()) {
		t.Fatal("floorplan catalog is nondeterministic")
	}
	if !reflect.DeepEqual(RegisteredPrimitives(), RegisteredPrimitives()) {
		t.Fatal("primitive registry is nondeterministic")
	}
	empty := FloorplanCatalog{}
	verdict := ValidateFloorplanCompatibility(PageComposition{Floorplan: "collection", FloorplanVersion: 1}, empty)
	if verdict.Compatible {
		t.Fatal("empty catalog validates a floorplan")
	}
	first := ValidateFloorplanCompatibility(PageComposition{Floorplan: "maze"}, RegisteredFloorplans())
	second := ValidateFloorplanCompatibility(PageComposition{Floorplan: "maze"}, RegisteredFloorplans())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("validation reasons are unstable")
	}
}
