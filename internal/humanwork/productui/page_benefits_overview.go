package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// benefitsOverviewPage is the route adapter for the
// benefit-program overview. The governed benefits service
// is not published to this UI yet, so the surface keeps
// the journeys fallback contract: an honest empty state
// with a recovery link that shows nothing — program truth
// stays server authority. The live overview composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func benefitsOverviewPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("benefits_overview.unavailable_title"),
		Description: view.Locale.Text("benefits_overview.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("benefits_overview.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
