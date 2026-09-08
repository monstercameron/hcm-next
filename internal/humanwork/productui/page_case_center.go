package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// caseCenterPage is the route adapter for the specialist
// Case Center. The governed help service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that centers nothing — case truth
// stays server authority. The live center composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func caseCenterPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("case_center.unavailable_title"),
		Description: view.Locale.Text("case_center.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("case_center.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
