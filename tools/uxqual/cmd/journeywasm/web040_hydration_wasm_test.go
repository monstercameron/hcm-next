//go:build js && wasm

package main

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_WEB_040_Hydration proves that the production shell has one
// reconciler-owned root while its route outlet changes from the SSR loading
// answer to resolved data. The search query is the cold URL that exposed the
// stale loading root in production.
func TestTodo_WEB_040_Hydration(t *testing.T) {
	browser := installWASMHistory(t, "/workspace/app/home?locale=en-US&menu_q=peple")
	// Initialize the UI facade before the fixture replaces its browser adapter.
	// A nil target is deliberately rejected without mounting a tree.
	if err := ui.RenderInto(nil, nil); err == nil {
		t.Fatal("nil render target unexpectedly accepted")
	}
	previousView := activeProductLayoutView
	previousHeading := activeProductLayoutShowHeading
	t.Cleanup(func() {
		activeProductLayoutView = previousView
		activeProductLayoutShowHeading = previousHeading
	})

	fixture := render.New(t, render.WithQueuedScheduler())
	defer fixture.Cleanup()
	loading := productui.NewView(productui.PageHome, "Tenant", "Taylor", "scope")
	loading.Loading = true
	setActiveProductLayout(loading, true)
	loadingRoot := ui.CreateElement(renderProductShellLayout, productShellLayoutProps{
		View: loading, ShowHeading: true,
		Outlet: productui.LoadingProxy(productui.LoadingProxyProps{Page: productui.PageHome}),
	})
	resolved := loading
	resolved.Loading = false

	serverMarkup, err := ui.RenderToString(loadingRoot)
	if err != nil {
		t.Fatal(err)
	}
	fixture.SeedHTML(serverMarkup)
	if _, err := ui.HydrateInto(loadingRoot, fixture.Target()); err != nil {
		t.Fatal(err)
	}
	fixture.Stabilize()
	fixture.FlushTimers()
	if got := len(fixture.AllByTag("header")); got != 1 {
		t.Fatalf("hydrated shell header count = %d, want one", got)
	}
	if got := len(fixture.AllByTag("main")); got != 1 {
		t.Fatalf("hydrated shell main count = %d, want one", got)
	}

	setActiveProductLayout(resolved, true)
	fixture.Rerender(ui.CreateElement(renderProductShellLayout, productShellLayoutProps{
		View: resolved, ShowHeading: true,
		Outlet: html.Section(html.Props{ID: "web040-resolved"}, ui.Text("resolved home")),
	}))
	fixture.Stabilize()
	fixture.FlushTimers()
	if fixture.ByID("web040-resolved") == nil {
		t.Fatalf("resolved data outlet did not replace the loading outlet: %s", fixture.Text())
	}
	if got := len(fixture.AllByTag("header")); got != 1 {
		t.Fatalf("resolved shell header count = %d, want one", got)
	}
	if got := len(fixture.AllByTag("main")); got != 1 {
		t.Fatalf("resolved shell main count = %d, want one", got)
	}
	if got := browser.path(); got != "/workspace/app/home?locale=en-US&menu_q=peple" {
		t.Fatalf("resolved search URL = %q, want original presentation URL", got)
	}
}
