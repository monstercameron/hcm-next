package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-088: the semantic outline editor. Composition is
// outline-first, but authors have no governed edits: primitives
// and regions change only by hand-editing slices with no
// vocabulary check and no atomicity. The lifecycle needs pure
// outline edits — add/remove primitives and regions — checked
// against the registered sets, applied atomically in order, with
// the resulting outline. Moves stay out: reordering is WEB-089,
// and ordering validation stays with WEB-076.
func TestTodo_WEB_088(t *testing.T) {
	start := PageComposition{Floorplan: "collection", Primitives: []string{"stack"}, Regions: []string{"primary"}}
	edited := ApplyOutlineEdits(start, []OutlineEdit{
		{Kind: OutlineAddPrimitive, Value: "table"},
		{Kind: OutlineRemovePrimitive, Value: "stack"},
		{Kind: OutlineAddRegion, Value: "supporting"},
	})
	if !edited.Compatible {
		t.Fatalf("governed edits refuse: %q", edited.Reasons)
	}
	if !reflect.DeepEqual(edited.Composition.Primitives, []string{"table"}) {
		t.Fatalf("primitives = %q", edited.Composition.Primitives)
	}
	if !reflect.DeepEqual(edited.Composition.Regions, []string{"primary", "supporting"}) {
		t.Fatalf("regions = %q", edited.Composition.Regions)
	}
	if edited.Composition.Floorplan != "collection" {
		t.Fatalf("editor rewrote non-outline fields: %+v", edited.Composition)
	}
	if !reflect.DeepEqual(start.Primitives, []string{"stack"}) {
		t.Fatal("editor mutated its input")
	}

	// Idempotent convergence: adding present and removing absent
	// succeed silently.
	converged := ApplyOutlineEdits(start, []OutlineEdit{
		{Kind: OutlineAddPrimitive, Value: "stack"},
		{Kind: OutlineRemovePrimitive, Value: "table"},
		{Kind: OutlineRemoveRegion, Value: "shell"},
	})
	if !converged.Compatible || !reflect.DeepEqual(converged.Composition.Primitives, []string{"stack"}) {
		t.Fatalf("convergent edits = (%t, %+v)", converged.Compatible, converged.Composition)
	}

	// Vocabulary violations fail the batch closed with stable
	// reasons; the outline comes back unchanged.
	for _, bad := range []struct {
		name   string
		edits  []OutlineEdit
		reason string
	}{
		{"unknown kind", []OutlineEdit{{Kind: "teleport", Value: "stack"}}, `unknown outline edit kind "teleport"`},
		{"unknown primitive", []OutlineEdit{{Kind: OutlineAddPrimitive, Value: "teleporter"}}, `unknown primitive "teleporter"`},
		{"unknown region", []OutlineEdit{{Kind: OutlineAddRegion, Value: "teleporter"}}, `unknown region "teleporter"`},
		{"platform region", []OutlineEdit{{Kind: OutlineAddRegion, Value: "shell"}}, `platform-owned region "shell"`},
	} {
		refused := ApplyOutlineEdits(start, bad.edits)
		if refused.Compatible {
			t.Fatalf("%s applies", bad.name)
		}
		found := false
		for _, reason := range refused.Reasons {
			if reason == bad.reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s reasons = %q, want %q", bad.name, refused.Reasons, bad.reason)
		}
		if !reflect.DeepEqual(refused.Composition, start) {
			t.Fatalf("%s mutated the outline: %+v", bad.name, refused.Composition)
		}
	}

	// Batches are atomic: one bad edit voids every edit.
	atomic := ApplyOutlineEdits(start, []OutlineEdit{
		{Kind: OutlineAddPrimitive, Value: "table"},
		{Kind: OutlineAddRegion, Value: "teleporter"},
	})
	if atomic.Compatible || !reflect.DeepEqual(atomic.Composition, start) {
		t.Fatalf("batch partially applied: (%t, %+v)", atomic.Compatible, atomic.Composition)
	}

	if empty := ApplyOutlineEdits(start, nil); !empty.Compatible || !reflect.DeepEqual(empty.Composition, start) {
		t.Fatalf("empty batch = (%t, %+v)", empty.Compatible, empty.Composition)
	}
}

// Golden: outline outcomes over an edit matrix.
func TestTodo_WEB_088_Golden(t *testing.T) {
	start := PageComposition{Primitives: []string{"stack", "table"}, Regions: []string{"primary"}}
	batches := [][]OutlineEdit{
		{{Kind: OutlineAddPrimitive, Value: "tabs"}, {Kind: OutlineAddRegion, Value: "supporting"}},
		{{Kind: OutlineRemovePrimitive, Value: "stack"}, {Kind: OutlineRemoveRegion, Value: "primary"}},
		{{Kind: OutlineAddPrimitive, Value: "teleporter"}},
		{{Kind: OutlineAddRegion, Value: "shell"}},
		{{Kind: "move-primitive", Value: "stack"}},
		{{Kind: OutlineAddPrimitive, Value: "stack"}, {Kind: OutlineAddPrimitive, Value: "teleporter"}},
		{},
	}
	var builder strings.Builder
	for _, batch := range batches {
		edited := ApplyOutlineEdits(start, batch)
		if edited.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(edited.Composition.Primitives, ","))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(edited.Composition.Regions, ","))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(edited.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "aad41cb9915c7d352abf028d766a35123c9c026c96c8ebaa8bbb13eb02627f1f"
	if got != want {
		t.Fatalf("outline digest = %s, want %s", got, want)
	}
}

// Browser: every registered primitive and every composable region
// adds to an empty outline — deterministically — and the shell
// stays unclaimable.
func TestTodo_WEB_088_Browser(t *testing.T) {
	for _, primitive := range RegisteredPrimitives() {
		first := ApplyOutlineEdits(PageComposition{}, []OutlineEdit{{Kind: OutlineAddPrimitive, Value: primitive}})
		second := ApplyOutlineEdits(PageComposition{}, []OutlineEdit{{Kind: OutlineAddPrimitive, Value: primitive}})
		if !first.Compatible || !reflect.DeepEqual(first.Composition.Primitives, []string{primitive}) {
			t.Fatalf("primitive %q does not add: %+v", primitive, first)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("primitive %q edit is nondeterministic", primitive)
		}
	}
	for _, region := range RegisteredRegions() {
		edited := ApplyOutlineEdits(PageComposition{}, []OutlineEdit{{Kind: OutlineAddRegion, Value: region.ID}})
		if !edited.Compatible || !reflect.DeepEqual(edited.Composition.Regions, []string{region.ID}) {
			t.Fatalf("region %q does not add: %+v", region.ID, edited)
		}
	}
	if refused := ApplyOutlineEdits(PageComposition{}, []OutlineEdit{{Kind: OutlineAddRegion, Value: "shell"}}); refused.Compatible {
		t.Fatal("shell composes through the editor")
	}
}

// Conformance: no aliasing, edit-order application, reason
// stability.
func TestTodo_WEB_088_Conformance(t *testing.T) {
	start := PageComposition{Primitives: []string{"stack"}, Regions: []string{"primary"}}
	first := ApplyOutlineEdits(start, []OutlineEdit{{Kind: OutlineAddPrimitive, Value: "table"}})
	first.Composition.Primitives[0] = "mutated"
	second := ApplyOutlineEdits(start, []OutlineEdit{{Kind: OutlineAddPrimitive, Value: "table"}})
	if !reflect.DeepEqual(second.Composition.Primitives, []string{"stack", "table"}) {
		t.Fatalf("editor aliases its output: %q", second.Composition.Primitives)
	}
	ordered := ApplyOutlineEdits(PageComposition{}, []OutlineEdit{
		{Kind: OutlineAddPrimitive, Value: "table"},
		{Kind: OutlineAddPrimitive, Value: "tabs"},
		{Kind: OutlineAddRegion, Value: "supporting"},
		{Kind: OutlineAddRegion, Value: "identity"},
	})
	if !reflect.DeepEqual(ordered.Composition.Primitives, []string{"table", "tabs"}) ||
		!reflect.DeepEqual(ordered.Composition.Regions, []string{"supporting", "identity"}) {
		t.Fatalf("edits apply out of order: %+v", ordered.Composition)
	}
	// Ordering validation stays downstream: the editor appends
	// without re-sorting.
	if verdict := ValidateRegionComposition(ordered.Composition); verdict.Compatible {
		t.Fatalf("editor silently fixed region order: %+v", ordered.Composition.Regions)
	}
	bad := ApplyOutlineEdits(start, []OutlineEdit{{Kind: OutlineAddPrimitive, Value: "teleporter"}})
	again := ApplyOutlineEdits(start, []OutlineEdit{{Kind: OutlineAddPrimitive, Value: "teleporter"}})
	if !reflect.DeepEqual(bad, again) {
		t.Fatal("outline reasons are unstable")
	}
}
