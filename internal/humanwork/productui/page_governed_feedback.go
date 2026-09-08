package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// governedFeedbackPage is the route adapter for governed
// feedback. The governed growth service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that exchanges nothing — feedback truth
// stays server authority. The live feedback composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func governedFeedbackPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("governed_feedback.unavailable_title"),
		Description: view.Locale.Text("governed_feedback.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("governed_feedback.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
