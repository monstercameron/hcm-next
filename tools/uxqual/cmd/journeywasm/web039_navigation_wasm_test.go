//go:build js && wasm

package main

import (
	"strings"
	"sync/atomic"
	"syscall/js"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/router"
	renderfixture "github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_WEB_039_Browser runs the compiled GWC runtime, drives its real
// HistoryRouter through a rendered software-navigation link, preserves the
// WEB-037 shell across projection changes, and proves authoritative empty and
// invalid answers never revive registry or role fallbacks.
func TestTodo_WEB_039_Browser(t *testing.T) {
	browser := installWASMHistory(t, productui.Path(productui.PageHome))
	var reloads atomic.Int32
	browser.addCallback("reload", func(js.Value, []js.Value) any {
		reloads.Add(1)
		return nil
	}, browser.location)

	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	productRouter.SetFocusManagement(false)
	productRouter.SetViewTransitions(false)
	for _, page := range []productui.PageID{productui.PageHome, productui.PagePeople} {
		page := page
		productRouter.Register(productui.Path(page), func(router.Attrs) *router.Element {
			return html.Section(html.Props{ID: "web039-router-" + string(page)})
		})
	}
	productRouter.Navigate(productui.Path(productui.PageHome))
	if productRouter.Current() == nil {
		t.Fatal("GWC HistoryRouter did not resolve the initial Home route")
	}

	fixture := renderfixture.New(t)
	view := web039WASMView(productui.PageHome, productRouter.Navigate)
	fixture.Render(ui.CreateElement(web039WASMNavigationShell, web039WASMNavigationShellProps{View: view, Outlet: "home"}))
	stable := map[string]int{}
	for _, id := range []string{"workspace-navigation", "main-content", "web039-shell-marker"} {
		node := fixture.ByID(id)
		if node == nil {
			t.Fatalf("initial compiled shell is missing %s", id)
		}
		stable[id] = node.NodeID()
	}
	banners := fixture.AllByTag("header")
	if len(banners) != 1 {
		t.Fatalf("initial compiled shell banner count = %d", len(banners))
	}
	stableBanner := banners[0].NodeID()

	people := fixture.ByRole("link", "People")
	if people == nil {
		t.Fatal("resolved People link did not mount in the compiled browser runtime")
	}
	people.Click()
	if got := browser.path(); got != productui.Path(productui.PagePeople) {
		t.Fatalf("GWC software navigation left browser at %q", got)
	}
	if reloads.Load() != 0 {
		t.Fatalf("GWC software navigation reloaded the document %d times", reloads.Load())
	}

	view = web039WASMView(productui.PagePeople, productRouter.Navigate)
	fixture.Rerender(ui.CreateElement(web039WASMNavigationShell, web039WASMNavigationShellProps{View: view, Outlet: "people"}))
	for id, nodeID := range stable {
		node := fixture.ByID(id)
		if node == nil || node.NodeID() != nodeID {
			t.Fatalf("WEB-037 stable shell node %s = %v, want identity %d", id, node, nodeID)
		}
	}
	banners = fixture.AllByTag("header")
	if len(banners) != 1 || banners[0].NodeID() != stableBanner {
		t.Fatalf("WEB-037 stable banner identity changed: %#v", banners)
	}
	people = fixture.ByRole("link", "People")
	if people == nil || people.Attr("aria-current") != "page" || fixture.ByID("web039-route-people") == nil || fixture.ByID("web039-route-home") != nil {
		t.Fatal("People software navigation did not update the active destination and route outlet")
	}

	empty := productui.ApplyNavigationProjection(productui.NewView(productui.PagePeople, "Tenant", "Taylor", "scope"), productui.AuthorizedNavigationProjection{Version: 4})
	empty.Navigate = productRouter.Navigate
	empty.Roles = []string{productui.RoleHCMAdmin}
	empty.FavoritePages = []productui.PageID{productui.PageAdmin, productui.PageSettings}
	fixture.Rerender(ui.CreateElement(web039WASMNavigationShell, web039WASMNavigationShellProps{View: empty, Outlet: "empty"}))
	assertWEB039AuthoritativeEmpty(t, fixture, "empty")

	invalid := productui.ApplyNavigationProjection(productui.NewView(productui.PagePeople, "Tenant", "Taylor", "scope"), productui.AuthorizedNavigationProjection{
		Version: 5,
		Items: []productui.AuthorizedNavigationItem{{
			Page: productui.PagePeople, Label: "People", LabelKey: "page.people.label", Icon: "people",
			Href: productui.Path(productui.PageAdmin), Authorized: true,
		}},
	})
	invalid.Navigate = productRouter.Navigate
	invalid.Roles = []string{productui.RoleHCMAdmin}
	fixture.Rerender(ui.CreateElement(web039WASMNavigationShell, web039WASMNavigationShellProps{View: invalid, Outlet: "invalid"}))
	assertWEB039AuthoritativeEmpty(t, fixture, "invalid")
	fixture.Cleanup()
}

func web039WASMView(page productui.PageID, navigate func(string)) productui.View {
	view := productui.NewView(page, "Tenant", "Taylor", "scope")
	view = productui.ApplyNavigationProjection(view, productui.AuthorizedNavigationProjection{
		Version: 3,
		Items: []productui.AuthorizedNavigationItem{
			{Page: productui.PageHome, Label: "Home", LabelKey: "page.home.label", Icon: "home", Href: productui.Path(productui.PageHome), Authorized: true},
			{Page: productui.PagePeople, Label: "People", LabelKey: "page.people.label", Icon: "people", Href: productui.Path(productui.PagePeople), Authorized: true},
		},
		Support: []productui.AuthorizedNavigationItem{{Page: productui.PageHelp, Label: "Help", LabelKey: "page.help.label", Icon: "help", Href: productui.Path(productui.PageHelp), Authorized: true}},
	})
	view.Navigate = navigate
	return view
}

func assertWEB039AuthoritativeEmpty(t *testing.T, fixture *renderfixture.Fixture, kind string) {
	t.Helper()
	navigation := fixture.ByID("workspace-navigation")
	if navigation == nil || !strings.Contains(navigation.Text(), "No navigation is available in this context.") || strings.Contains(navigation.Text(), "No menus match") {
		text := "<missing>"
		if navigation != nil {
			text = navigation.Text()
		}
		t.Fatalf("%s authoritative projection did not render its safe empty navigation state: %q", kind, text)
	}
	for _, label := range []string{"Home", "People", "Admin", "Help", "Settings", "Myself"} {
		if fixture.ByRole("link", label) != nil {
			t.Fatalf("%s authoritative projection revived %s", kind, label)
		}
	}
	for _, link := range fixture.AllByTag("a") {
		for _, forbidden := range []string{"/workspace/app/admin", "/workspace/app/help", "/workspace/app/settings", "/workspace/app/myself", "favorites=admin"} {
			if href := link.Attr("href"); strings.Contains(href, forbidden) {
				t.Fatalf("%s authoritative projection leaked %q through %q", kind, forbidden, href)
			}
		}
	}
}

type web039WASMNavigationShellProps struct {
	View   productui.View
	Outlet string
}

func web039WASMNavigationShell(props web039WASMNavigationShellProps) ui.Node {
	return html.Div(html.Props{ID: "web039-shell-marker"}, productui.BuildShell(props.View,
		html.Section(html.Props{ID: "web039-route-" + props.Outlet}, ui.Text("Route content")), true,
	))
}
