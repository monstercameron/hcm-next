package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// activationReadinessPage is the route adapter for
// worker-activation readiness. The governed activation
// service is not published to this UI yet, so the surface
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that activates nothing —
// activation stays server authority. The live readiness
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func activationReadinessPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("activation_readiness.unavailable_title"),
		Description: view.Locale.Text("activation_readiness.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("activation_readiness.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
