package workspace

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/journeyclient"
)

// NavElementID is the id of the shell's navigation landmark.
const NavElementID = "workspace-nav"

// Nav builds the shell's navigation region from route, the current address
// as tools/uxqual/journeyclient.Parse would have produced it. It never
// parses or constructs a hash address itself -- journeyclient.ListHref,
// WorkerHref, and DetailHref remain the one place those strings are built --
// this function only decides, from an already-parsed Route, which of the
// links it emits should carry aria-current.
//
// It always includes a link back to the unfiltered journeys list. When
// route also names a selected worker (RouteList with WorkerRef set) or an
// open journey (RouteDetail), it adds one more link naming that selection,
// marked current, so the navigation region always states in words what the
// address bar currently means.
func Nav(route journeyclient.Route) ui.Node {
	items := []ui.Node{
		navItem("Promotion journeys", journeyclient.ListHref(), route.Kind == journeyclient.RouteList && route.WorkerRef == ""),
	}
	switch route.Kind {
	case journeyclient.RouteList:
		if route.WorkerRef != "" {
			items = append(items, navItem("Selected worker: "+route.WorkerRef, journeyclient.WorkerHref(route.WorkerRef), true))
		}
	case journeyclient.RouteDetail:
		if route.IntentID != "" {
			items = append(items, navItem("Journey: "+route.IntentID, journeyclient.DetailHref(route.IntentID), true))
		}
	}
	return html.Nav(html.Props{ID: NavElementID, Aria: map[string]string{"label": "Primary"}},
		html.Ul(html.Props{}, items...),
	)
}

func navItem(label, href string, current bool) ui.Node {
	props := html.Props{Href: href}
	if current {
		props.Aria = map[string]string{"current": "page"}
	}
	return html.Li(html.Props{}, html.A(props, ui.Text(label)))
}
