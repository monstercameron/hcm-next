package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// exitInitiationPage is the route adapter for exit
// initiation. The governed lifecycle service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that initiates nothing — exit truth
// stays server authority. The live initiation
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func exitInitiationPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("exit_initiation.unavailable_title"),
		Description: view.Locale.Text("exit_initiation.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("exit_initiation.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
