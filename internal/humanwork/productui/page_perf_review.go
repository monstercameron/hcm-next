package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// perfReviewPage is the route adapter for the
// performance-review workspace. The governed growth
// service is not published to this UI yet, so the surface
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that reviews nothing —
// review truth stays server authority. The live workspace
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func perfReviewPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("perf_review.unavailable_title"),
		Description: view.Locale.Text("perf_review.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("perf_review.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
