package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// helpHubPage is the route adapter for the employee
// Help hub. The governed help service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that helps with nothing — help truth
// stays server authority. The live Help composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func helpHubPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("help_hub.unavailable_title"),
		Description: view.Locale.Text("help_hub.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("help_hub.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
