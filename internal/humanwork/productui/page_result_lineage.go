package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// resultLineagePage is the route adapter for result
// lineage presentation. The governed reporting service
// is not published to this UI yet, so the surface keeps
// the journeys fallback contract: an honest empty state
// with a recovery link that presents nothing — lineage
// truth stays server authority. The live lineage
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func resultLineagePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("result_lineage.unavailable_title"),
		Description: view.Locale.Text("result_lineage.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("result_lineage.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
