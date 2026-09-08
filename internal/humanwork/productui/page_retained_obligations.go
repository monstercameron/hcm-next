package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// retainedObligationsPage is the route adapter for
// retained-obligation presentation. The governed
// lifecycle service is not published to this UI yet, so
// the surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that presents
// nothing — obligation truth stays server authority.
// The live presentation composition replaces this body
// once the governed service publishes; until then the UI
// will not simulate one.
func retainedObligationsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("retained_obligations.unavailable_title"),
		Description: view.Locale.Text("retained_obligations.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("retained_obligations.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
