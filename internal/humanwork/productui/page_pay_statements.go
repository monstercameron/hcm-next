package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// payStatementsPage is the route adapter for accessible
// pay statements. The governed pay service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — statement truth
// stays server authority. The live statements composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func payStatementsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("pay_statements.unavailable_title"),
		Description: view.Locale.Text("pay_statements.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("pay_statements.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
