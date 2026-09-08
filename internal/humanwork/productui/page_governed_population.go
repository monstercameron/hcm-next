package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// governedPopulationPage is the route adapter for
// governed population building. The governed headcount
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that builds
// nothing — population truth stays server authority.
// The live population composition replaces this body
// once the governed service publishes; until then the UI
// will not simulate one.
func governedPopulationPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("governed_population.unavailable_title"),
		Description: view.Locale.Text("governed_population.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("governed_population.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
