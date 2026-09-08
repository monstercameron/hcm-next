package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// successionPlanningPage is the route adapter for
// succession planning. The governed growth service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that plans nothing — succession truth
// stays server authority. The live planning composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func successionPlanningPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("succession_planning.unavailable_title"),
		Description: view.Locale.Text("succession_planning.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("succession_planning.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
