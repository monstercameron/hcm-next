// Package productui governs the worker object page's
// responsive layout. Pixels stay in CSS; Go resolves which
// layout a viewport width selects and which panes a layout
// shows. The narrow breakpoint mirrors the stylesheet's own
// collapse of .person-layout to one column at 1040px. The
// zero layout is unknown: widths at or below zero resolve
// narrow so an unreadable width stacks instead of clipping,
// and unknown layouts show the detail stack only so the
// page keeps its core facts. Both known layouts show both
// panes — the detail stack and the workflow launcher rail —
// and CSS owns their stacking. The route adapter carries no
// viewport width today, so this is the governed resolution
// point its future wiring will call.
package productui

// WorkerPageNarrowMaxWidth is the widest viewport, in CSS
// pixels, that selects the narrow worker page layout. It
// mirrors the typed stylesheet's MaxW(1040) collapse of
// .person-layout to a single column.
const WorkerPageNarrowMaxWidth = 1040

// WorkerPageLayout is the responsive layout behind the
// worker object page. The zero value is unknown and fails
// closed to the detail stack so content is never stranded.
type WorkerPageLayout int

const (
	// WorkerPageWide shows the detail stack and launcher rail side by side.
	WorkerPageWide WorkerPageLayout = iota + 1
	// WorkerPageNarrow stacks the detail stack above the launcher rail.
	WorkerPageNarrow
)

// ResolveWorkerPageLayout selects the worker page layout
// for a viewport width in CSS pixels. Widths at or below
// zero are unknown and fail closed to narrow.
func ResolveWorkerPageLayout(viewportWidthPx int) WorkerPageLayout {
	if viewportWidthPx <= 0 || viewportWidthPx <= WorkerPageNarrowMaxWidth {
		return WorkerPageNarrow
	}
	return WorkerPageWide
}

// ResolveWorkerPagePanes decides which worker page panes a
// layout shows: the detail stack and the workflow launcher
// rail. Known layouts show both; unknown layouts show the
// detail stack only.
func ResolveWorkerPagePanes(layout WorkerPageLayout) (showDetails, showLauncher bool) {
	switch layout {
	case WorkerPageWide:
		return true, true
	case WorkerPageNarrow:
		return true, true
	}
	return true, false
}
