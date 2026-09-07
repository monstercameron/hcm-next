package floorplan

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestTodo_WEB_023(t *testing.T) {
	fp := promotionLaunch()
	for _, tc := range []struct {
		breakpoint Breakpoint
		columns    int
		stacked    bool
	}{
		{BreakpointNarrow, 1, true},
		{BreakpointCompact, 1, false},
		{BreakpointStandard, 2, false},
		{BreakpointWide, 3, false},
	} {
		layout, err := fp.TransformAt(tc.breakpoint)
		if err != nil {
			t.Fatalf("TransformAt(%s): %v", tc.breakpoint, err)
		}
		if len(layout.Regions) != len(fp.Regions) || layout.Regions[2].Region.Name != "workforce" {
			t.Fatalf("responsive transform changed region order: %+v", layout.Regions)
		}
		workforce := layout.Regions[2]
		if workforce.Region.Layout.Mode != LayoutGrid || workforce.Region.Layout.MinColumns != tc.columns || workforce.Region.Layout.MaxColumns != tc.columns || workforce.Stacked != tc.stacked {
			t.Fatalf("%s workforce layout = %+v, want %d columns stacked=%v", tc.breakpoint, workforce, tc.columns, tc.stacked)
		}
	}
	if fp.ResponsiveRules[0].Columns != 1 || fp.Regions[2].Layout.MaxColumns != 2 {
		t.Fatal("TransformAt mutated the registered floorplan")
	}
}

func TestTodo_WEB_023_Golden(t *testing.T) {
	fp := promotionIntentWorkspace()
	first, err := fp.TransformAt(BreakpointNarrow)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fp.TransformAt(BreakpointNarrow)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Canonical()) != string(second.Canonical()) || first.Digest() != second.Digest() {
		t.Fatal("responsive layout transform is not deterministic")
	}
	// The digest is intentionally computed from the semantic result only;
	// widgets, records, authorization facts and viewport pixels cannot affect
	// this contract.
	const want = "sha256:ffa97b9ace0d91286308bb7582ba2c6a3634591a936d7a75d14c8432152272f6"
	if first.Digest() != want {
		t.Fatalf("narrow intent workspace digest = %s, want %s", first.Digest(), want)
	}
}

func TestResponsiveTransformationBounds(t *testing.T) {
	// Renderer adapters consume semantic metadata; this test protects the
	// closed vocabulary and narrow fallback before it reaches a renderer.
	fp := promotionIntentWorkspace()
	for _, b := range []Breakpoint{BreakpointNarrow, BreakpointCompact, BreakpointStandard, BreakpointWide} {
		layout, err := fp.TransformAt(b)
		if err != nil {
			t.Fatal(err)
		}
		for _, region := range layout.Regions {
			if region.Region.Layout.MinColumns < 1 || region.Region.Layout.MaxColumns > 12 || region.Region.Layout.MaxColumns < region.Region.Layout.MinColumns {
				t.Fatalf("%s emitted unbounded region %+v", b, region)
			}
		}
	}
}

func TestTodo_WEB_023_Conformance(t *testing.T) {
	if _, err := promotionLaunch().TransformAt(Breakpoint("phone")); !errors.Is(err, ErrInvalidFloorplan) {
		t.Fatalf("unknown breakpoint error = %v, want ErrInvalidFloorplan", err)
	}
	base := promotionIntentWorkspace()
	for _, b := range Breakpoints() {
		one, err := base.TransformAt(b)
		if err != nil {
			t.Fatal(err)
		}
		two, err := base.TransformAt(b)
		if err != nil {
			t.Fatal(err)
		}
		if string(one.Canonical()) != string(two.Canonical()) {
			t.Fatalf("repeated TransformAt diverges at %s", b)
		}
		if strings.Contains(string(one.Canonical()), "<") || strings.Contains(string(one.Canonical()), "permission") {
			t.Fatalf("layout contract leaked markup or authority vocabulary at %s", b)
		}
	}
}

func TestResponsiveTransformationKeepsNarrowFallbackWithoutIntermediateRule(t *testing.T) {
	fp := promotionLaunch()
	fp.ResponsiveRules = []ResponsiveRule{
		{Breakpoint: BreakpointWide, Region: "workforce", Mode: LayoutGrid, Columns: 3},
	}
	for _, breakpoint := range []Breakpoint{BreakpointNarrow, BreakpointCompact, BreakpointStandard} {
		layout, err := fp.TransformAt(breakpoint)
		if err != nil {
			t.Fatal(err)
		}
		if got := layout.Regions[2].Region.Layout.MaxColumns; got != 1 {
			t.Fatalf("%s restored wide base before a widening rule: got %d columns", breakpoint, got)
		}
	}
}

func TestResponsiveTransformationIgnoresRuleDeclarationOrder(t *testing.T) {
	ordered := promotionIntentWorkspace()
	shuffled := promotionIntentWorkspace()
	for left, right := 0, len(shuffled.ResponsiveRules)-1; left < right; left, right = left+1, right-1 {
		shuffled.ResponsiveRules[left], shuffled.ResponsiveRules[right] = shuffled.ResponsiveRules[right], shuffled.ResponsiveRules[left]
	}
	for _, breakpoint := range Breakpoints() {
		want, err := ordered.TransformAt(breakpoint)
		if err != nil {
			t.Fatal(err)
		}
		got, err := shuffled.TransformAt(breakpoint)
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Canonical()) != string(want.Canonical()) {
			t.Fatalf("%s projection depends on responsive rule declaration order", breakpoint)
		}
	}
}

func TestResponsiveTransformationRejectsInvalidEffectiveLayout(t *testing.T) {
	fp := promotionLaunch()
	fp.ResponsiveRules = []ResponsiveRule{
		{Breakpoint: BreakpointCompact, Region: "shell", Columns: 2},
	}
	if err := fp.Validate(); !errors.Is(err, ErrInvalidFloorplan) {
		t.Fatalf("invalid effective flow layout error = %v, want ErrInvalidFloorplan", err)
	}
}

func TestResponsiveTransformationLatencyGate(t *testing.T) {
	fp := promotionIntentWorkspace()
	durations := make([]time.Duration, 1000)
	for i := range durations {
		start := time.Now()
		if _, err := fp.TransformAt(BreakpointWide); err != nil {
			t.Fatal(err)
		}
		durations[i] = time.Since(start)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)*95)/100]
	if p95 > time.Millisecond {
		t.Fatalf("responsive transformation p95 = %s, exceeding 1ms budget", p95)
	}
}

func BenchmarkResponsiveLayoutTransformation(b *testing.B) {
	fp := promotionIntentWorkspace()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := fp.TransformAt(BreakpointWide); err != nil {
			b.Fatal(err)
		}
	}
}
