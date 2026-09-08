package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-089: keyboard page-region reordering. Outline edits
// (WEB-088) add and remove, but regions cannot move: drag
// operations have no keyboard-accessible counterpart, so keyboard
// authors cannot reorder at all. The lifecycle needs a pure
// single-step region move — one region, one step earlier or
// later — with boundary moves converging silently (the
// disabled-control equivalent), unknown regions and directions
// refusing. Directions stay axis-neutral (earlier/later, never
// left/right or up/down) so the same controls hold in vertical,
// horizontal, and RTL layouts.
func TestTodo_WEB_089(t *testing.T) {
	start := PageComposition{Regions: []string{"identity", "primary", "supporting"}}
	earlier := MoveOutlineRegion(start, RegionMove{Region: "primary", Direction: RegionMoveEarlier})
	if !earlier.Compatible {
		t.Fatalf("earlier move refuses: %q", earlier.Reasons)
	}
	if !reflect.DeepEqual(earlier.Composition.Regions, []string{"primary", "identity", "supporting"}) {
		t.Fatalf("earlier move = %q", earlier.Composition.Regions)
	}
	later := MoveOutlineRegion(start, RegionMove{Region: "primary", Direction: RegionMoveLater})
	if !later.Compatible || !reflect.DeepEqual(later.Composition.Regions, []string{"identity", "supporting", "primary"}) {
		t.Fatalf("later move = (%t, %q)", later.Compatible, later.Composition.Regions)
	}

	// Boundary moves converge silently, like pressing a disabled
	// control.
	for _, edge := range []RegionMove{
		{Region: "identity", Direction: RegionMoveEarlier},
		{Region: "supporting", Direction: RegionMoveLater},
	} {
		converged := MoveOutlineRegion(start, edge)
		if !converged.Compatible || !reflect.DeepEqual(converged.Composition, start) {
			t.Fatalf("boundary move %+v = (%t, %+v)", edge, converged.Compatible, converged.Composition)
		}
	}

	for _, bad := range []struct {
		name   string
		move   RegionMove
		reason string
	}{
		{"unknown region", RegionMove{Region: "teleporter", Direction: RegionMoveEarlier}, `unknown region "teleporter"`},
		{"blank region", RegionMove{Direction: RegionMoveEarlier}, `unknown region ""`},
		{"unknown direction", RegionMove{Region: "primary", Direction: "sideways"}, `unknown move direction "sideways"`},
		{"blank direction", RegionMove{Region: "primary"}, `unknown move direction ""`},
	} {
		refused := MoveOutlineRegion(start, bad.move)
		if refused.Compatible {
			t.Fatalf("%s moves", bad.name)
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
			t.Fatalf("%s mutated the outline", bad.name)
		}
	}

	// Duplicates move the first occurrence; other fields pass
	// through.
	dupes := PageComposition{Floorplan: "collection", Regions: []string{"supporting", "primary", "supporting"}}
	moved := MoveOutlineRegion(dupes, RegionMove{Region: "supporting", Direction: RegionMoveLater})
	if !moved.Compatible || !reflect.DeepEqual(moved.Composition.Regions, []string{"primary", "supporting", "supporting"}) {
		t.Fatalf("duplicate move = (%t, %q)", moved.Compatible, moved.Composition.Regions)
	}
	if moved.Composition.Floorplan != "collection" {
		t.Fatalf("move rewrote non-outline fields: %+v", moved.Composition)
	}
}

// Golden: move outcomes over a move matrix.
func TestTodo_WEB_089_Golden(t *testing.T) {
	start := PageComposition{Regions: []string{"identity", "primary", "supporting"}}
	moves := []RegionMove{
		{Region: "primary", Direction: RegionMoveEarlier},
		{Region: "primary", Direction: RegionMoveLater},
		{Region: "identity", Direction: RegionMoveEarlier},
		{Region: "supporting", Direction: RegionMoveLater},
		{Region: "teleporter", Direction: RegionMoveEarlier},
		{Region: "primary", Direction: "sideways"},
		{Region: "identity", Direction: RegionMoveLater},
		{Region: "supporting", Direction: RegionMoveEarlier},
	}
	var builder strings.Builder
	for _, move := range moves {
		moved := MoveOutlineRegion(start, move)
		if moved.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(moved.Composition.Regions, ","))
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(moved.Reasons, ";"))
		builder.WriteString("\n")
	}
	// Sequential keyboard walk: earlier, earlier, later.
	walk := start
	for _, move := range []RegionMove{
		{Region: "supporting", Direction: RegionMoveEarlier},
		{Region: "supporting", Direction: RegionMoveEarlier},
		{Region: "supporting", Direction: RegionMoveLater},
	} {
		walk = MoveOutlineRegion(walk, move).Composition
	}
	builder.WriteString("walk\x00" + strings.Join(walk.Regions, ",") + "\n")
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "638461aa3bd168aebd1c4c76db8966fdace363e606c0e24b04d38d496bf9dd1b"
	if got != want {
		t.Fatalf("reorder digest = %s, want %s", got, want)
	}
}

// Browser: every composable region round-trips — one step earlier
// plus one step later returns the full anatomy — deterministically.
func TestTodo_WEB_089_Browser(t *testing.T) {
	anatomy := []string{}
	for _, region := range RegisteredRegions() {
		anatomy = append(anatomy, region.ID)
	}
	full := PageComposition{Regions: anatomy}
	for i, region := range RegisteredRegions() {
		away := MoveOutlineRegion(full, RegionMove{Region: region.ID, Direction: RegionMoveEarlier})
		again := MoveOutlineRegion(full, RegionMove{Region: region.ID, Direction: RegionMoveEarlier})
		if !away.Compatible {
			t.Fatalf("region %q does not move: %q", region.ID, away.Reasons)
		}
		if !reflect.DeepEqual(away, again) {
			t.Fatalf("region %q move is nondeterministic", region.ID)
		}
		back := MoveOutlineRegion(away.Composition, RegionMove{Region: region.ID, Direction: RegionMoveLater})
		if i == 0 {
			// The first region converges at the boundary: away is
			// a no-op, so later moves it one step in.
			if !reflect.DeepEqual(back.Composition.Regions, []string{"authority", "identity", "navigation", "primary", "supporting", "utility", "completion"}) {
				t.Fatalf("boundary round-trip = %q", back.Composition.Regions)
			}
			continue
		}
		if !reflect.DeepEqual(back.Composition.Regions, anatomy) {
			t.Fatalf("region %q round-trip = %q", region.ID, back.Composition.Regions)
		}
	}
}

// Conformance: no aliasing, single-step granularity, reason
// stability.
func TestTodo_WEB_089_Conformance(t *testing.T) {
	start := PageComposition{Regions: []string{"identity", "primary", "supporting"}}
	first := MoveOutlineRegion(start, RegionMove{Region: "supporting", Direction: RegionMoveEarlier})
	first.Composition.Regions[0] = "mutated"
	second := MoveOutlineRegion(start, RegionMove{Region: "supporting", Direction: RegionMoveEarlier})
	if !reflect.DeepEqual(second.Composition.Regions, []string{"identity", "supporting", "primary"}) {
		t.Fatalf("move aliases its output: %q", second.Composition.Regions)
	}
	// One step only: supporting jumps a single slot, never to the
	// front in one move.
	if !reflect.DeepEqual(second.Composition.Regions, []string{"identity", "supporting", "primary"}) {
		t.Fatalf("move jumped more than one step: %q", second.Composition.Regions)
	}
	bad := MoveOutlineRegion(start, RegionMove{Region: "primary", Direction: "sideways"})
	again := MoveOutlineRegion(start, RegionMove{Region: "primary", Direction: "sideways"})
	if !reflect.DeepEqual(bad, again) {
		t.Fatal("move reasons are unstable")
	}
}
