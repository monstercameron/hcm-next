package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// analysisHandoffPage is the route adapter for
// analysis-to-proposal handoff. The governed reporting
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that hands off
// nothing — handoff truth stays server authority. The
// live handoff composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func analysisHandoffPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("analysis_handoff.unavailable_title"),
		Description: view.Locale.Text("analysis_handoff.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("analysis_handoff.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
