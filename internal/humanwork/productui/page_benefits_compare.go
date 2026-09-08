package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// benefitsComparePage is the route adapter for
// benefit-plan comparison. The governed benefits service
// is not published to this UI yet, so the surface keeps
// the journeys fallback contract: an honest empty state
// with a recovery link that compares nothing — plan truth
// stays server authority. The live comparison composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func benefitsComparePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("benefits_compare.unavailable_title"),
		Description: view.Locale.Text("benefits_compare.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("benefits_compare.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
