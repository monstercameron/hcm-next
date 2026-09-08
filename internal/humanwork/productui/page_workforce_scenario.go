package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// workforceScenarioPage is the route adapter for
// workforce scenario authoring. The governed headcount
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that authors
// nothing — scenario truth stays server authority. The
// live scenario composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func workforceScenarioPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("workforce_scenario.unavailable_title"),
		Description: view.Locale.Text("workforce_scenario.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("workforce_scenario.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
