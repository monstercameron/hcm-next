package workspace

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/floorplan"
	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/page"
)

// RootElementID is the id of the shell's own root node.
const RootElementID = "workspace-shell"

// SkipTargetElementID is the id the shell's skip link targets: the
// PageDefinition's Primary region landmark, which
// tools/uxqual/render/page.Render always gives the id "main-content" (the
// same convention tools/uxqual/ssrshell uses), regardless of which region
// happens to be Primary on a given page.
const SkipTargetElementID = "main-content"

// Input is everything [Build] needs to compose the canonical Intent
// Workspace shell around one governed page.
type Input struct {
	// Resolution is the page to render inside the shell: a PageDefinition
	// already validated and resolved against its floorplan (see
	// tools/uxqual/floorplan.Registry.Resolve).
	Resolution floorplan.Resolution
	// Widgets resolves every widget ref Resolution.Page declares. See
	// tools/uxqual/render/page.Render.
	Widgets *page.Registry
	// Route is the current hash route
	// (tools/uxqual/journeyclient.Route, produced by journeyclient.Parse),
	// which drives the navigation region (see [Nav]).
	Route journeyclient.Route
	// Session is the acting principal's display facts, read from the
	// page's session island (see [ReadSession]). A zero Session renders no
	// authority-context strip at all (see [SessionStrip]).
	Session Session
}

// Build composes the canonical Intent Workspace shell as one GWC component
// tree, in the frontend plan's own page-anatomy order:
//
//  1. a skip link targeting the page's Primary landmark;
//  2. the navigation region ([Nav]), driven by in.Route;
//  3. the authority-context strip ([SessionStrip]), driven by in.Session;
//  4. the page's own rendered tree (tools/uxqual/render/page.Render),
//     including that governed page's one canonical live region.
//
// It never falls back to free HTML for a page.Render failure: an
// unregistered widget ref, or a PageDefinition that fails validation, is
// returned as an error -- wrapped, not swallowed -- rather than rendered as
// a partial shell around a missing body. Every piece above is otherwise a
// pure function of its own inputs (see each one's doc comment), so Build
// itself is a pure function of in: called twice with an equal Input it
// renders to identical bytes (see this package's golden test).
func Build(in Input) (ui.Node, error) {
	body, err := page.Render(in.Resolution, in.Widgets)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	return html.Div(html.Props{ID: RootElementID},
		skipLink(),
		Nav(in.Route),
		SessionStrip(in.Session),
		body,
	), nil
}

func skipLink() ui.Node {
	return html.A(html.Props{Class: "visually-hidden", Href: "#" + SkipTargetElementID},
		ui.Text("Skip to main content"),
	)
}
