package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// releaseGatePage is the route adapter for the
// production frontend release gate. The governed release
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that gates
// nothing — release truth stays server authority. The
// live gate composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func releaseGatePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("release_gate.unavailable_title"),
		Description: view.Locale.Text("release_gate.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("release_gate.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
