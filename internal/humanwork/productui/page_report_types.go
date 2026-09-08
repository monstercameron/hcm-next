package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reportTypesPage is the route adapter for the certified
// and customer report distinction. The governed
// reporting service is not published to this UI yet, so
// the surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that
// distinguishes nothing — report truth stays server
// authority. The live distinction composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func reportTypesPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("report_types.unavailable_title"),
		Description: view.Locale.Text("report_types.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("report_types.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
