package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reassignmentReviewPage is the route adapter for manager
// and work reassignment review. The governed lifecycle
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that reviews
// nothing — reassignment truth stays server authority.
// The live review composition replaces this body once
// the governed service publishes; until then the UI will
// not simulate one.
func reassignmentReviewPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("reassignment_review.unavailable_title"),
		Description: view.Locale.Text("reassignment_review.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("reassignment_review.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
