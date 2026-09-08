package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// orgExplorerPage is the route adapter for the
// organization explorer. The governed organization
// service is not published to this UI yet, so the surface
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that explores nothing — org
// truth stays server authority. The live explorer
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func orgExplorerPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("org_explorer.unavailable_title"),
		Description: view.Locale.Text("org_explorer.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("org_explorer.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
