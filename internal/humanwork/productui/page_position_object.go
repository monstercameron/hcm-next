package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// positionObjectPage is the route adapter for the
// position object page. The governed position service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — position truth
// stays server authority. The live position composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func positionObjectPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("position_object.unavailable_title"),
		Description: view.Locale.Text("position_object.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("position_object.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
