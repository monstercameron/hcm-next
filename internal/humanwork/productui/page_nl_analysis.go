package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// nlAnalysisPage is the route adapter for safe
// natural-language analysis. The governed reporting
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that answers
// nothing — answer truth stays server authority. The
// live analysis composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func nlAnalysisPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("nl_analysis.unavailable_title"),
		Description: view.Locale.Text("nl_analysis.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("nl_analysis.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
