package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// payDiscrepancyPage is the route adapter for
// pay-discrepancy intake. The governed pay service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that files nothing — case truth stays
// server authority. The live intake composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func payDiscrepancyPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("pay_discrepancy.unavailable_title"),
		Description: view.Locale.Text("pay_discrepancy.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("pay_discrepancy.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
