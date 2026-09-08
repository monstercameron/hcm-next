package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// dataFreshnessPage is the route adapter for
// data-freshness presentation. The governed reporting
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that presents
// nothing — freshness truth stays server authority. The
// live freshness composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func dataFreshnessPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("data_freshness.unavailable_title"),
		Description: view.Locale.Text("data_freshness.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("data_freshness.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
