package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// timeHubPage is the route adapter for the employee time
// hub. The governed time service is not published to this
// UI yet, so the surface keeps the journeys fallback
// contract: an honest empty state with a recovery link
// that records nothing — time truth stays server
// authority. The live hub composition replaces this body
// once the governed service publishes; until then the UI
// will not simulate one.
func timeHubPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("time_hub.unavailable_title"),
		Description: view.Locale.Text("time_hub.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("time_hub.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
