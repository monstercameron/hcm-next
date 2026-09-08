package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reportExportPage is the route adapter for governed
// report export. The governed reporting service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that exports nothing — export truth
// stays server authority. The live export composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func reportExportPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("report_export.unavailable_title"),
		Description: view.Locale.Text("report_export.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("report_export.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
