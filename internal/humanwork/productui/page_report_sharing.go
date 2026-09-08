package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reportSharingPage is the route adapter for authorized
// report sharing. The governed reporting service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shares nothing — sharing truth
// stays server authority. The live sharing composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func reportSharingPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("report_sharing.unavailable_title"),
		Description: view.Locale.Text("report_sharing.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("report_sharing.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
