package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-120: the responsive worker object page. The
// stylesheet collapses the worker layout to one column at
// 1040px, but no typed Go governs the page's layout: the
// first consumer re-implements the breakpoint by convention
// and unknown widths pick a layout by accident. The
// compiler needs the governed layout — viewport widths
// resolving against the stylesheet's own collapse point
// with unknown widths failing closed, plus the pane
// decision for each layout — so narrow pages stack without
// stranding either pane.
func TestTodo_WEB_120(t *testing.T) {
	if WorkerPageNarrowMaxWidth != 1040 {
		t.Fatalf("narrow breakpoint = %d, want the stylesheet collapse at 1040", WorkerPageNarrowMaxWidth)
	}
	for width, want := range map[int]WorkerPageLayout{
		1440: WorkerPageWide, 1041: WorkerPageWide,
		1040: WorkerPageNarrow, 390: WorkerPageNarrow, 320: WorkerPageNarrow,
		0: WorkerPageNarrow, -1: WorkerPageNarrow,
	} {
		if got := ResolveWorkerPageLayout(width); got != want {
			t.Fatalf("layout(%d) = %v, want %v", width, got, want)
		}
	}

	showDetails, showLauncher := ResolveWorkerPagePanes(WorkerPageWide)
	if !showDetails || !showLauncher {
		t.Fatal("wide hides a worker pane")
	}
	showDetails, showLauncher = ResolveWorkerPagePanes(WorkerPageNarrow)
	if !showDetails || !showLauncher {
		t.Fatal("narrow strands a worker pane")
	}
	showDetails, showLauncher = ResolveWorkerPagePanes(WorkerPageLayout(0))
	if !showDetails || showLauncher {
		t.Fatal("unknown layout does not fail closed to the detail stack")
	}
}

// Golden: layouts and panes over representative widths.
func TestTodo_WEB_120_Golden(t *testing.T) {
	widths := []int{-10, 0, 320, 390, 760, 1040, 1041, 1440, 3840}
	var builder strings.Builder
	for _, width := range widths {
		layout := ResolveWorkerPageLayout(width)
		showDetails, showLauncher := ResolveWorkerPagePanes(layout)
		fmt.Fprintf(&builder, "%d|%d|%t|%t\n", width, int(layout), showDetails, showLauncher)
	}
	for _, layout := range []WorkerPageLayout{WorkerPageLayout(0), WorkerPageLayout(99)} {
		showDetails, showLauncher := ResolveWorkerPagePanes(layout)
		fmt.Fprintf(&builder, "layout-%d|%t|%t\n", int(layout), showDetails, showLauncher)
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "0aa3e4fa1a3632248edb981b861671209ff7210aa28e14149a81ffc8b831e7e3"
	if got != want {
		t.Fatalf("worker page layout digest = %s, want %s", got, want)
	}
}

// Browser: layout resolution is pure and deterministic.
func TestTodo_WEB_120_Browser(t *testing.T) {
	for _, width := range []int{0, 320, 1040, 1041, 2880} {
		first := ResolveWorkerPageLayout(width)
		second := ResolveWorkerPageLayout(width)
		if first != second {
			t.Fatalf("layout(%d) is nondeterministic", width)
		}
		firstDetails, firstLauncher := ResolveWorkerPagePanes(first)
		secondDetails, secondLauncher := ResolveWorkerPagePanes(second)
		if firstDetails != secondDetails || firstLauncher != secondLauncher {
			t.Fatalf("panes(%d) are nondeterministic", width)
		}
	}
}

// Conformance: the breakpoint is a single typed constant,
// layouts are distinct, resolution is stable.
func TestTodo_WEB_120_Conformance(t *testing.T) {
	if WorkerPageWide == WorkerPageNarrow {
		t.Fatal("layouts collapse")
	}
	if WorkerPageWide == WorkerPageLayout(0) || WorkerPageNarrow == WorkerPageLayout(0) {
		t.Fatal("zero value names a layout instead of unknown")
	}
	boundary := ResolveWorkerPageLayout(WorkerPageNarrowMaxWidth)
	above := ResolveWorkerPageLayout(WorkerPageNarrowMaxWidth + 1)
	if boundary != WorkerPageNarrow || above != WorkerPageWide {
		t.Fatal("resolution does not pivot on the typed breakpoint")
	}
	if !reflect.DeepEqual(ResolveWorkerPageLayout(390), ResolveWorkerPageLayout(390)) {
		t.Fatal("resolution is unstable")
	}
}
