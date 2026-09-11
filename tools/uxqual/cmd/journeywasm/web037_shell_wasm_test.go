//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall/js"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/router"
	renderfixture "github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_WEB_037_Browser runs in Go's js/wasm runtime and uses GWC's
// controlled DOM adapter. It seeds server markup, hydrates it, and then drives
// the real HistoryRouter through two software navigations.
func TestTodo_WEB_037_Browser(t *testing.T) {
	browser := installWASMHistory(t, productui.Path(productui.PageHome))
	var reloads atomic.Int32
	browser.addCallback("reload", func(js.Value, []js.Value) any {
		reloads.Add(1)
		return nil
	}, browser.location)

	previousLayoutView := activeProductLayoutView
	previousShowHeading := activeProductLayoutShowHeading
	previousFocusedRoute := lastFocusedProductRoute
	previousResolvedView := lastResolvedProductView
	t.Cleanup(func() {
		activeProductLayoutView = previousLayoutView
		activeProductLayoutShowHeading = previousShowHeading
		lastFocusedProductRoute = previousFocusedRoute
		lastResolvedProductView = previousResolvedView
	})

	fixture := renderfixture.New(t)
	var shellMountSerial atomic.Int32
	home := web037BrowserView(productui.PageHome)
	home.Loading = true
	setActiveProductLayout(home, true)
	// Router factories execute before GWC enters a component context. Pin both
	// production factories to component elements so a future direct call to
	// BuildShell or BuildPageContent cannot reintroduce hook-outside-component
	// panics for software-navigation callbacks.
	boundaries := []struct {
		name    string
		element *router.Element
	}{
		{name: "shell", element: productShellLayoutComponent(nil)},
		{name: "leaf", element: productRouteComponent(nil)},
	}
	for _, boundary := range boundaries {
		if boundary.element == nil {
			t.Fatalf("%s route factory returned no component boundary", boundary.name)
		}
		if _, isHostElement := boundary.element.Type.(string); isHostElement {
			t.Fatalf("%s route factory rendered a host element outside component context", boundary.name)
		}
	}
	serverTree := ui.CreateElement(web037StatefulShellLayout, web037ShellLayoutProps{
		Outlet:      productui.LoadingProxy(productui.LoadingProxyProps{Page: productui.PageHome}),
		MountSerial: &shellMountSerial,
	})
	serverMarkup, err := ui.RenderToString(serverTree)
	if err != nil {
		t.Fatal(err)
	}
	seeded := fixture.SeedHTML(serverMarkup)
	if seeded.NodeIDs["web037-shell-boundary"] == 0 || seeded.NodeIDs["web037-shell-local-state"] == 0 ||
		seeded.NodeIDs["workspace-navigation"] == 0 || seeded.NodeIDs["main-content"] == 0 {
		t.Fatalf("server shell landmark identities = %+v", seeded.NodeIDs)
	}
	serverBanners := fixture.AllByTag("header")
	if len(serverBanners) != 1 {
		t.Fatalf("server shell banner count = %d, want one", len(serverBanners))
	}
	serverBannerID := serverBanners[0].NodeID()
	if _, err := ui.HydrateInto(serverTree, fixture.Target()); err != nil {
		t.Fatal(err)
	}
	fixture.Stabilize()
	banners := fixture.AllByTag("header")
	if len(banners) != 1 {
		t.Fatal("hydrated shell has no banner landmark")
	}
	bannerID := banners[0].NodeID()
	assertWEB037NodeIdentity(t, fixture, seeded.NodeIDs, serverBannerID, "after hydration")
	fixture.Cleanup()

	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	productRouter.SetFocusManagement(false)
	productRouter.SetViewTransitions(false)
	productRouter.Register("/workspace/app", func(router.Attrs) *router.Element {
		return html.Div(html.Props{ID: "web037-router-layout"}, router.GetOutlet())
	}, router.Options{Layout: true})
	registerWEB037Route := func(page productui.PageID) {
		productRouter.Register(productui.Path(page), func(attrs router.Attrs) *router.Element {
			return html.Section(html.Props{ID: "web037-router-leaf-" + string(page)})
		}, router.Options{
			Title: web037BrowserView(page).Title + " · Human Capital Management Suite",
		})
	}
	registerWEB037Route(productui.PageHome)
	registerWEB037Route(productui.PagePeople)
	registerWEB037Route(productui.PageSettings)
	if productRouter.Current() == nil {
		t.Fatal("HistoryRouter did not resolve Home")
	}
	// Prime GWC's browser router runtime before installing the controlled DOM
	// fixture. The router and render fixture intentionally share the global
	// reconciler; first-time router initialization after the fixture would
	// replace its adapter and make later fixture updates invisible.
	productRouter.Navigate(productui.Path(productui.PageHome))

	// Use a fresh controlled runtime for navigation so the browser-router and
	// reconciler lifecycle is independent of the one-shot hydration fixture.
	fixture = renderfixture.New(t)
	shellMountSerial.Store(0)
	fixture.Render(ui.CreateElement(web037StatefulShellLayout, web037ShellLayoutProps{
		Outlet:      productui.LoadingProxy(productui.LoadingProxyProps{Page: productui.PageHome}),
		MountSerial: &shellMountSerial,
	}))
	banners = fixture.AllByTag("header")
	if len(banners) != 1 {
		t.Fatal("mounted shell has no banner landmark")
	}
	bannerID = banners[0].NodeID()
	shellBoundary := fixture.ByID("web037-shell-boundary")
	shellMarkerNode := fixture.ByID("web037-shell-local-state")
	navigation := fixture.ByID("workspace-navigation")
	main := fixture.ByID("main-content")
	if shellBoundary == nil || shellMarkerNode == nil || navigation == nil || main == nil {
		t.Fatalf("mounted shell landmarks missing: ids=%v", web037FixtureIDs(fixture.Container()))
	}
	stableIDs := map[string]int{
		"web037-shell-boundary":    shellBoundary.NodeID(),
		"web037-shell-local-state": shellMarkerNode.NodeID(),
		"workspace-navigation":     navigation.NodeID(),
		"main-content":             main.NodeID(),
	}
	shellMarker := shellMarkerNode.Text()
	shellMarkerID := shellMarkerNode.NodeID()
	if shellMarker == "" {
		t.Fatal("mounted shell component-local state marker was empty")
	}
	probeView := web037BrowserView(productui.PagePeople)
	setActiveProductLayout(probeView, true)
	probeOutlet := html.Section(html.Props{ID: "web037-outlet-probe"})
	fixture.Rerender(ui.CreateElement(web037StatefulShellLayout, web037ShellLayoutProps{Outlet: probeOutlet, MountSerial: &shellMountSerial}))
	if fixture.ByID("web037-outlet-probe") == nil {
		t.Fatalf("production shell component boundary retained its initial outlet: ids=%v mounts=%d", web037FixtureIDs(fixture.Container()), shellMountSerial.Load())
	}
	setActiveProductLayout(home, true)
	homeOutlet := html.Section(html.Props{ID: "web037-outlet-home"}, html.H2(html.Props{}, ui.Text(home.Title)))
	fixture.Rerender(ui.CreateElement(web037StatefulShellLayout, web037ShellLayoutProps{Outlet: homeOutlet, MountSerial: &shellMountSerial}))

	renderShellPage := func(page productui.PageID) {
		view := web037BrowserView(page)
		setActiveProductLayout(view, true)
		outlet := html.Section(html.Props{ID: "web037-outlet-" + string(page), Aria: map[string]string{"label": "Route outlet"}},
			html.H2(html.Props{}, ui.Text(view.Title)),
		)
		fixture.Rerender(ui.CreateElement(web037StatefulShellLayout, web037ShellLayoutProps{Outlet: outlet, MountSerial: &shellMountSerial}))
	}

	// The router selects an independent leaf while the reconciler updates the
	// production shell composition around that outlet.
	renderShellPage(productui.PageHome)
	assertWEB037NodeIdentity(t, fixture, stableIDs, bannerID, "after initial route")
	assertWEB037LocalMarker(t, fixture, shellMarker, shellMarkerID)

	productRouter.Navigate(productui.Path(productui.PagePeople) + "?q=slow")
	if productRouter.Current() == nil {
		t.Fatal("HistoryRouter did not resolve People")
	}
	renderShellPage(productui.PagePeople)
	if fixture.ByID("web037-outlet-people") == nil || fixture.ByID("web037-outlet-home") != nil {
		t.Fatalf("People navigation did not replace Home: path=%q inspect=%+v ids=%v", browser.path(), router.InspectCurrentRoute(), web037FixtureIDs(fixture.Container()))
	}
	assertWEB037NodeIdentity(t, fixture, stableIDs, bannerID, "after People navigation")
	assertWEB037LocalMarker(t, fixture, shellMarker, shellMarkerID)

	productRouter.Navigate(productui.Path(productui.PageSettings))
	if productRouter.Current() == nil {
		t.Fatal("HistoryRouter did not resolve Settings")
	}
	renderShellPage(productui.PageSettings)

	if got := browser.path(); got != productui.Path(productui.PageSettings) {
		t.Fatalf("software navigation left browser at %q", got)
	}
	if reloads.Load() != 0 {
		t.Fatalf("software navigation reloaded the document %d times", reloads.Load())
	}
	assertWEB037NodeIdentity(t, fixture, stableIDs, bannerID, "after software navigation")
	assertWEB037LocalMarker(t, fixture, shellMarker, shellMarkerID)
	homeOutletNode := fixture.ByID("web037-outlet-home")
	peopleOutlet := fixture.ByID("web037-outlet-people")
	settingsOutlet := fixture.ByID("web037-outlet-settings")
	if homeOutletNode != nil || peopleOutlet != nil || settingsOutlet == nil {
		t.Fatalf("route outlet replacement: home=%v people=%v settings=%v ids=%v", homeOutletNode != nil, peopleOutlet != nil, settingsOutlet != nil, web037FixtureIDs(fixture.Container()))
	}
	heading := fixture.ByID("page-title")
	if heading == nil || heading.Text() != web037BrowserView(productui.PageSettings).Title || heading.Attr("tabindex") != "-1" {
		t.Fatalf("Settings focus heading = %#v", heading)
	}
	settings := web037BrowserView(productui.PageSettings)
	announcement := settings.Locale.Text("shell.page_loaded", map[string]string{"title": settings.Title})
	liveRegions := 0
	for _, statusNode := range fixture.AllByRole("status") {
		if statusNode.Attr("aria-live") == "polite" && statusNode.Text() == announcement {
			liveRegions++
		}
	}
	if liveRegions != 1 {
		t.Fatalf("Settings route announcement count = %d, want one", liveRegions)
	}

	// Loader cancellation is orthogonal to DOM reconciliation. A deliberately
	// non-cooperative People loader may finish late, but its generation must be
	// canceled and may not displace the already-visible Settings address.
	peopleStarted := make(chan struct{}, 1)
	peopleCanceled := make(chan struct{}, 1)
	releasePeople := make(chan struct{})
	var releasePeopleOnce sync.Once
	t.Cleanup(func() { releasePeopleOnce.Do(func() { close(releasePeople) }) })
	cancellationRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageSettings)})
	cancellationRouter.SetFocusManagement(false)
	cancellationRouter.SetViewTransitions(false)
	cancellationRouter.Register(productui.Path(productui.PagePeople), func(router.Attrs) *router.Element {
		return html.Div(html.Props{ID: "stale-people-answer"})
	}, router.Options{
		Loader: func(ctx context.Context, _ router.RouteContext) (router.Attrs, error) {
			peopleStarted <- struct{}{}
			<-ctx.Done()
			peopleCanceled <- struct{}{}
			<-releasePeople
			return router.Attrs{}, nil
		},
		Loading: func(router.Attrs) *router.Element { return html.Div(html.Props{ID: "slow-people-loading"}) },
	})
	cancellationRouter.Register(productui.Path(productui.PageSettings), func(router.Attrs) *router.Element {
		return html.Div(html.Props{ID: "current-settings-answer"})
	})
	cancellationRouter.Navigate(productui.Path(productui.PagePeople) + "?q=slow")
	wantWEB037Signal(t, peopleStarted, "People loader did not start")
	cancellationRouter.Navigate(productui.Path(productui.PageSettings))
	wantWEB037Signal(t, peopleCanceled, "superseded People generation was not canceled")
	releasePeopleOnce.Do(func() { close(releasePeople) })
	if got := browser.path(); got != productui.Path(productui.PageSettings) {
		t.Fatalf("stale route generation changed browser address to %q", got)
	}
}

func web037FixtureIDs(node *renderfixture.QueryNode) []string {
	if node == nil {
		return nil
	}
	ids := []string{}
	if id := node.Attr("id"); id != "" {
		ids = append(ids, id)
	}
	for _, child := range node.Children() {
		ids = append(ids, web037FixtureIDs(child)...)
	}
	return ids
}

func web037BrowserView(page productui.PageID) productui.View {
	view := productui.ApplyRoleVisibility(productui.NewView(page, "Tenant", "Taylor", "manager"), []string{"manager"})
	view.Viewer = productui.ViewerProfile{Name: "Taylor", Role: "Manager"}
	view.Navigate = router.Navigate
	view.NavigateDebounced = func(string) {}
	return view
}

type web037ShellLayoutProps struct {
	Outlet      ui.Node
	MountSerial *atomic.Int32
}

func web037StatefulShellLayout(props web037ShellLayoutProps) ui.Node {
	serial := int32(0)
	if props.MountSerial != nil {
		serial = props.MountSerial.Add(1)
	}
	marker := ui.UseState(strconv.FormatInt(int64(serial), 10))
	return html.Div(html.Props{ID: "web037-shell-boundary"},
		html.Tag("output", html.Props{ID: "web037-shell-local-state"}, ui.Text(marker.Get())),
		ui.CreateElement(renderProductShellLayout, productShellLayoutProps{Outlet: props.Outlet, View: activeProductLayoutView, ShowHeading: activeProductLayoutShowHeading}),
	)
}

func wantWEB037Signal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func assertWEB037NodeIdentity(t *testing.T, fixture *renderfixture.Fixture, seeded map[string]int, bannerID int, phase string) {
	t.Helper()
	for _, id := range []string{"web037-shell-boundary", "web037-shell-local-state", "workspace-navigation", "main-content"} {
		node := fixture.ByID(id)
		if node == nil || node.NodeID() != seeded[id] {
			t.Fatalf("%s %s node identity = %v, want %d", phase, id, node, seeded[id])
		}
	}
	banners := fixture.AllByTag("header")
	if len(banners) != 1 || banners[0].NodeID() != bannerID {
		t.Fatalf("%s banner node identity = %v, want %d", phase, banners, bannerID)
	}
}

func assertWEB037LocalMarker(t *testing.T, fixture *renderfixture.Fixture, want string, wantNodeID int) {
	t.Helper()
	input := fixture.ByID("web037-shell-local-state")
	if input == nil || input.Text() != want || input.NodeID() != wantNodeID {
		got := "<missing>"
		if input != nil {
			got = input.Text()
		}
		t.Fatalf("shell-local navigation state = %q on node %v, want %q on node %d", got, input, want, wantNodeID)
	}
}
