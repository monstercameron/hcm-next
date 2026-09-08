package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// analysisFiltersPage is the route adapter for
// authorized analysis filters. The governed reporting
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that filters
// nothing — filter truth stays server authority. The
// live filter composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func analysisFiltersPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("analysis_filters.unavailable_title"),
		Description: view.Locale.Text("analysis_filters.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("analysis_filters.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
