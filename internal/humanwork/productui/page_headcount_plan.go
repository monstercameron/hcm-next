package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// headcountPlanPage is the route adapter for the
// headcount-plan workspace. The governed headcount
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that plans
// nothing — plan truth stays server authority. The live
// headcount composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func headcountPlanPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("headcount_plan.unavailable_title"),
		Description: view.Locale.Text("headcount_plan.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("headcount_plan.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
