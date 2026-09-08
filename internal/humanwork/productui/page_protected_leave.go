package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// protectedLeavePage is the route adapter for
// protected-leave intake. The governed leave service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that opens nothing — case truth stays
// server authority. The live intake composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func protectedLeavePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("protected_leave.unavailable_title"),
		Description: view.Locale.Text("protected_leave.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("protected_leave.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
