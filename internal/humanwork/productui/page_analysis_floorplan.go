package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// analysisFloorplanPage is the route adapter for the
// analysis floorplan. The governed reporting service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that lays out nothing — analysis truth
// stays server authority. The live floorplan composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func analysisFloorplanPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("analysis_floorplan.unavailable_title"),
		Description: view.Locale.Text("analysis_floorplan.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("analysis_floorplan.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
