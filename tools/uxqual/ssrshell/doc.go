// Package ssrshell is WEB-025's server-side renderer: it turns a validated
// tools/uxqual/pagedef.PageDefinition into a semantic HTML shell document.
//
// It sits directly downstream of pagedef in the frontend pipeline
// (planning/specs/production-frontend-and-page-composition.md, "Frontend
// runtime and transport"):
//
//	pagedef.PageDefinition (governed, versioned contract)
//	    -> ssrshell.Render (this package: semantic HTML shell, no widget
//	       content, no business data)
//	    -> a floorplan/widget-registry-aware layer (WEB-003/WEB-005, not
//	       built yet) fills each empty widget-slot mount point with real,
//	       authorization-filtered content
//	    -> optionally, Go/WASM progressively enhances the document in place
//	       (WEB-027 and later)
//
// # What this package does and does not own
//
// ssrshell owns exactly one thing: projecting a PageDefinition's structure
// (regions, region kinds, headings, widget references, accessibility
// requirements) onto semantic HTML markup. It never:
//
//   - resolves a widget id to real widget content (WEB-005's job) -- every
//     widget slot renders empty, identified only by its widget ref;
//   - resolves a data binding or fires an RPC -- a DataBinding/ActionRef is
//     not rendered as visible markup at all, only as data the caller can act
//     on once the region's real widgets are mounted;
//   - resolves a brand token to a color, font, or spacing value -- BrandTokens
//     are named in the PageDefinition but never consulted by this package;
//     ShellCSS is a small, brand-token-free utility stylesheet (a
//     visually-hidden helper class for the skip link), not a themed page
//     style;
//   - emits any inline, executable script. The only <script> element this
//     package ever writes has type="application/json" -- a data island, not
//     a program -- so [ContentSecurityPolicy] states script-src 'none' as
//     an accurate policy, not a placeholder.
//
// # Determinism
//
// [Render] is a pure function of its PageDefinition argument: the same input
// always produces the same output bytes and the same [RenderedShell.Digest].
// It performs no clock read, no random ID generation, and no I/O.
//
// # Integration point (not wired by this package)
//
// The one place in this repository that already serves a page shell over
// HTTP is internal/humanwork/workspace/journey_shell.go's serveJourney,
// which hand-builds a much simpler, purpose-specific shell (a JSON
// configuration island plus a WASM client loader) for the single live
// Promotion journey page. That file is owned by another lane's file roots
// and is not modified here; a later todo wiring a governed, PageDefinition-
// driven route would:
//
//  1. build (or look up) the page's pagedef.PageDefinition;
//  2. call ssrshell.Render(pd) to get the semantic shell and its digest;
//  3. set the response's Content-Security-Policy header from
//     ssrshell.ContentSecurityPolicy();
//  4. hand the still-empty widget-slot mount points to whatever
//     floorplan/widget-registry layer WEB-003/WEB-005 define, the same way
//     journey_shell.go's own JSON island is the handoff point to its WASM
//     client today.
//
// No such route exists yet: WEB-003 (floorplan registry) and WEB-005
// (widget registry) are still open todos this package does not depend on
// and cannot resolve against.
package ssrshell
