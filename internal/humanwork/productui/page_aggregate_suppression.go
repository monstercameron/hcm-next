package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// aggregateSuppressionPage is the route adapter for
// aggregate suppression states. The governed reporting
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that
// suppresses nothing — suppression truth stays server
// authority. The live suppression composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func aggregateSuppressionPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("aggregate_suppression.unavailable_title"),
		Description: view.Locale.Text("aggregate_suppression.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("aggregate_suppression.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
