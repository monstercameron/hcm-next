package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// evaluationPage is the route adapter for structured
// candidate evaluation. The governed evaluation service
// is not published to this UI yet, so the page keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that exposes no criteria, ratings, or
// recommendations. The live evaluation composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func evaluationPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("evaluation.unavailable_title"),
		Description: view.Locale.Text("evaluation.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("evaluation.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
