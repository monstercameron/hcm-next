package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// paySummaryPage is the route adapter for the employee
// pay summary. The governed pay service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that shows nothing — pay truth stays
// server authority. The live summary composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func paySummaryPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("pay_summary.unavailable_title"),
		Description: view.Locale.Text("pay_summary.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("pay_summary.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
