// Package workspace is WEB-121's canonical Intent Workspace shell: the
// governed GWC chrome every Intent Workspace floorplan page renders inside,
// composed from pieces this repository has already qualified rather than
// invented fresh for this todo.
//
//	tools/uxqual/render/page.Render (WEB-026: one page's own tree)
//	    + this package's navigation region (driven by the hash routes
//	      tools/uxqual/journeyclient already parses and builds:
//	      "#/journeys", "#/journeys?worker=<ref>")
//	    + this package's session/authority-context reader ([ReadSession])
//	    -> Build: the whole shell as one GWC component tree
//
// # What this package does and does not own
//
// workspace owns exactly one thing: assembling the three pieces above into
// one deterministic tree, in the frontend plan's own page-anatomy order
// (shell chrome, then the page's own regions starting at page identity). It
// never:
//
//   - resolves a widget ref, a data binding, or an RPC -- that is entirely
//     [tools/uxqual/render/page.Render]'s job; this package hands it a
//     pre-resolved floorplan.Resolution and a widget [page.Registry] and
//     propagates its error unchanged rather than rendering a partial shell
//     around a failure. The page renderer also owns the governed page's one
//     canonical live region; Build does not add a second shell announcer;
//   - parses or owns hash routing -- [tools/uxqual/journeyclient.Parse] and
//     Href/ListHref/WorkerHref/DetailHref remain the one place a route and
//     an address translate into each other; this package only reads an
//     already-parsed [journeyclient.Route] to decide what the navigation
//     region says is current;
//   - reads or displays a credential -- [Session] has no field for one (see
//     session.go), so even a session island that also carries a bearer (the
//     one this repository's journey shell writes today) cannot flow through
//     this package's reader into rendered markup.
//
// # Integration point (not wired by this package)
//
// internal/humanwork/workspace/journey_shell.go's serveJourney today serves
// a bespoke, journey-specific shell (a JSON configuration island plus a
// wasm client loader) rather than this package's canonical one, and
// tools/uxqual/cmd/journeywasm's client mounts
// tools/uxqual/render/journey.LiveComponent directly rather than this
// package's Build. Neither file is owned by this package's lane, and
// neither is modified by it. The reported integration steps to swap them
// over are the corresponding todo's evidence, not a change made here.
package workspace
