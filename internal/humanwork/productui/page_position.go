package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// positionPage is the route adapter for position requests.
// The governed position service is not published yet, so
// the page keeps the journeys fallback contract: an
// honest empty state with a recovery link that exposes no
// positions, openings, or vacancies. The live position
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func positionPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("position.unavailable_title"),
		Description: view.Locale.Text("position.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("position.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
