package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// growthHomePage is the route adapter for the employee
// Growth home. The governed growth service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — growth truth stays
// server authority. The live home composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func growthHomePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("growth_home.unavailable_title"),
		Description: view.Locale.Text("growth_home.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("growth_home.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
