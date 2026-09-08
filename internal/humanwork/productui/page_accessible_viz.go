package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// accessibleVizPage is the route adapter for accessible
// data visualization. The governed reporting service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that visualizes nothing —
// visualization truth stays server authority. The live
// visualization composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func accessibleVizPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("accessible_viz.unavailable_title"),
		Description: view.Locale.Text("accessible_viz.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("accessible_viz.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
