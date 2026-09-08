package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// goalPlanningPage is the route adapter for goal
// planning. The governed growth service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that plans nothing — goal truth stays
// server authority. The live planning composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func goalPlanningPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("goal_planning.unavailable_title"),
		Description: view.Locale.Text("goal_planning.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("goal_planning.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
