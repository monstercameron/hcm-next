package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// benefitsEnrollPage is the route adapter for benefit
// enrollment. The governed benefits service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that enrolls nothing — enrollment truth
// stays server authority. The live enrollment composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func benefitsEnrollPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("benefits_enroll.unavailable_title"),
		Description: view.Locale.Text("benefits_enroll.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("benefits_enroll.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
