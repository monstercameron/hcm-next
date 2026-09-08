package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reportCatalogPage is the route adapter for the report
// catalog. The governed reporting service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that catalogs nothing — report truth
// stays server authority. The live catalog composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func reportCatalogPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("report_catalog.unavailable_title"),
		Description: view.Locale.Text("report_catalog.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("report_catalog.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
