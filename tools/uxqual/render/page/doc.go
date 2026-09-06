// Package page is WEB-026's deterministic GWC page renderer: it turns a
// validated tools/uxqual/pagedef.PageDefinition, resolved against a
// tools/uxqual/floorplan.Floorplan, into a GoWebComponents (GWC) component
// tree by mounting each declared widget slot from a caller-supplied
// [Registry], in the page's own definition order.
//
// It sits directly downstream of the same two contracts
// tools/uxqual/ssrshell (WEB-025) renders, and reuses ssrshell's own
// landmark mapping ([ssrshell.LandmarkForRegionKind]) so the two renderers
// can never disagree about which HTML element a given region kind becomes:
//
//	pagedef.PageDefinition + floorplan.Resolution (governed, versioned contracts)
//	    -> ssrshell.Render (semantic HTML shell, empty widget-slot mount points)
//	    -> page.Render (this package: the SAME landmark structure, with each
//	       widget slot filled by a real, registered GWC component instead of
//	       an empty placeholder div)
//	    -> Go/WASM mounts the tree live (WEB-027 and later)
//
// # What this package does and does not own
//
// page owns exactly one thing: projecting a PageDefinition's structure
// (regions, region kinds, headings, widget slots) onto a GWC component tree
// and mounting each slot's registered widget into it, in document order. It
// never:
//
//   - resolves a widget id on its own authority -- [Registry] is an
//     explicit, caller-supplied port; an unregistered widget ref is refused
//     with a typed [*UnregisteredWidgetError], never silently skipped or
//     rendered as free HTML;
//   - fetches, filters, or authorizes any business data -- a widget
//     constructor is handed only the [WidgetContext] this package derives
//     from the PageDefinition itself (page id/version, the region, the
//     slot), never a live RPC answer;
//   - resolves a floorplan reference -- the caller resolves the
//     PageDefinition against a [floorplan.Registry] first and hands this
//     package the resulting [floorplan.Resolution], the same division of
//     labor tools/uxqual/floorplan.Registry.Resolve already enforces.
//
// # Determinism
//
// [Render] is a pure function of its (floorplan.Resolution, *Registry)
// arguments, provided every registered [Widget] is itself pure: called
// twice with an equal [WidgetContext], a registered widget must return trees
// that render to identical bytes through GWC's native ui.RenderToString
// path (GWC's own SSR writer sorts every element's attributes before
// writing them, so map-valued props never introduce nondeterminism on their
// own -- see internal/runtime/ssr.go in the GWC module). This package
// performs no clock read, no random ID generation, and no I/O, so the same
// (PageDefinition, Floorplan, Registry) always produces the same tree and
// therefore the same rendered digest; see the golden test for the two real
// Promotion pages.
package page
