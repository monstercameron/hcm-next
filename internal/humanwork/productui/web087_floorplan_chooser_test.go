package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// RED for WEB-087: the governed floorplan chooser. Compatibility
// validation (WEB-075) checks a composed floorplan, but the
// authoring flow has no governed choice step: nothing resolves an
// author's floorplan pick against the catalog, pins the version,
// or refuses unregistered picks with the catalog's wording. The
// lifecycle needs a pure chooser — catalog, pick, optional
// version — returning the pinned choice or a stable refusal.
// Purpose and audience do not narrow the catalog: no such rule is
// stated, and inventing one would be a second authority.
func TestTodo_WEB_087(t *testing.T) {
	catalog := RegisteredFloorplans()
	choice, err := ChooseFloorplan(catalog, "collection", 0)
	if err != nil {
		t.Fatalf("registered pick refuses: %v", err)
	}
	if choice != (FloorplanChoice{Floorplan: "collection", Version: 1}) {
		t.Fatalf("choice pins %+v, want collection at catalog version", choice)
	}

	for _, bad := range []struct {
		name    string
		id      string
		version int64
		want    string
	}{
		{"unknown floorplan", "teleporter", 0, `unknown floorplan "teleporter"`},
		{"blank floorplan", "", 0, `unknown floorplan ""`},
		{"future version", "collection", 9, `unsupported floorplan version 9 for "collection"`},
		{"negative version", "collection", -1, `unsupported floorplan version -1 for "collection"`},
	} {
		if _, err := ChooseFloorplan(catalog, bad.id, bad.version); err == nil || err.Error() != bad.want {
			t.Fatalf("%s = %v, want %q", bad.name, err, bad.want)
		}
	}

	// An explicit older version pins as asked: the renderer admits
	// versioned floorplans, and staying back is the author's call.
	bumped := RegisteredFloorplans()
	for i := range bumped.Floorplans {
		if bumped.Floorplans[i].ID == "collection" {
			bumped.Floorplans[i].Version = 3
		}
	}
	older, err := ChooseFloorplan(bumped, "collection", 1)
	if err != nil {
		t.Fatalf("older version refuses: %v", err)
	}
	if older != (FloorplanChoice{Floorplan: "collection", Version: 1}) {
		t.Fatalf("older choice pins %+v", older)
	}
	if _, err := ChooseFloorplan(bumped, "collection", 4); err == nil {
		t.Fatal("version past the bumped catalog chooses")
	}
}

// Golden: chooser outcomes over a pick matrix against a bumped
// catalog.
func TestTodo_WEB_087_Golden(t *testing.T) {
	catalog := RegisteredFloorplans()
	for i := range catalog.Floorplans {
		catalog.Floorplans[i].Version = 2
	}
	picks := []struct {
		id      string
		version int64
	}{
		{"collection", 0},
		{"collection", 1},
		{"collection", 2},
		{"collection", 3},
		{"teleporter", 0},
		{"", 1},
		{"object", -1},
		{"object", 2},
	}
	var builder strings.Builder
	for _, pick := range picks {
		choice, err := ChooseFloorplan(catalog, pick.id, pick.version)
		builder.WriteString(pick.id)
		builder.WriteString("\x00")
		if err != nil {
			builder.WriteString("refused")
			builder.WriteString("\x00")
			builder.WriteString(err.Error())
		} else {
			builder.WriteString("chosen")
			builder.WriteString("\x00")
			builder.WriteString(choice.Floorplan)
			builder.WriteString("\x00")
			builder.WriteString(strconv.FormatInt(choice.Version, 10))
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "4a334fbdba99a47977679801e350fdcec72476b00da142e9c91dbeca1dcfcc91"
	if got != want {
		t.Fatalf("chooser digest = %s, want %s", got, want)
	}
}

// Browser: every registered floorplan chooses at the catalog
// version — deterministically — and the pinned choice composes
// through compatibility validation.
func TestTodo_WEB_087_Browser(t *testing.T) {
	catalog := RegisteredFloorplans()
	for _, floorplan := range catalog.Floorplans {
		first, err := ChooseFloorplan(catalog, floorplan.ID, 0)
		if err != nil {
			t.Fatalf("floorplan %q does not choose: %v", floorplan.ID, err)
		}
		second, err := ChooseFloorplan(catalog, floorplan.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if first != (FloorplanChoice{Floorplan: floorplan.ID, Version: floorplan.Version}) {
			t.Fatalf("floorplan %q pins %+v", floorplan.ID, first)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("floorplan %q choice is nondeterministic", floorplan.ID)
		}
		composition := PageComposition{Floorplan: first.Floorplan, FloorplanVersion: first.Version}
		if verdict := ValidateFloorplanCompatibility(composition, catalog); !verdict.Compatible {
			t.Fatalf("floorplan %q pinned choice fails validation: %q", floorplan.ID, verdict.Reasons)
		}
	}
}

// Conformance: the chooser never mutates the catalog, errors are
// stable, empty catalogs refuse everything.
func TestTodo_WEB_087_Conformance(t *testing.T) {
	catalog := RegisteredFloorplans()
	before := RegisteredFloorplans()
	if _, err := ChooseFloorplan(catalog, "collection", 0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(catalog, before) {
		t.Fatal("chooser mutated the catalog")
	}
	_, firstErr := ChooseFloorplan(catalog, "teleporter", 0)
	_, secondErr := ChooseFloorplan(catalog, "teleporter", 0)
	if firstErr == nil || secondErr == nil || firstErr.Error() != secondErr.Error() {
		t.Fatal("chooser errors are unstable")
	}
	if _, err := ChooseFloorplan(FloorplanCatalog{}, "collection", 0); err == nil {
		t.Fatal("empty catalog chooses")
	}
	// Explicit current pins identically to unpinned.
	explicit, err := ChooseFloorplan(catalog, "collection", 1)
	if err != nil {
		t.Fatal(err)
	}
	unpinned, err := ChooseFloorplan(catalog, "collection", 0)
	if err != nil {
		t.Fatal(err)
	}
	if explicit != unpinned {
		t.Fatalf("explicit current %+v differs from unpinned %+v", explicit, unpinned)
	}
}
