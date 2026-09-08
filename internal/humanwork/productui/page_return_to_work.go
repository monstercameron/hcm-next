package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// returnToWorkPage is the route adapter for return-to-work
// planning. The governed leave service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that plans nothing — plan truth stays
// server authority. The live planning composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func returnToWorkPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("return_to_work.unavailable_title"),
		Description: view.Locale.Text("return_to_work.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("return_to_work.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
