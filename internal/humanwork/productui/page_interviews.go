package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// interviewsPage is the route adapter for interview
// scheduling. The governed scheduling service is not
// published to this UI yet, so the page keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that exposes no slots, interviewers,
// or confirmations. The live scheduling composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func interviewsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("interviews.unavailable_title"),
		Description: view.Locale.Text("interviews.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("interviews.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
