package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// managerCheckinsPage is the route adapter for manager
// check-ins. The governed growth service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that runs nothing — check-in truth stays
// server authority. The live check-ins composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func managerCheckinsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("manager_checkins.unavailable_title"),
		Description: view.Locale.Text("manager_checkins.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("manager_checkins.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
