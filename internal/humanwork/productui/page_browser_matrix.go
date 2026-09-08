package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// browserMatrixPage is the route adapter for the
// production browser matrix. The governed qualification
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that qualifies
// nothing — matrix truth stays server authority. The
// live matrix composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func browserMatrixPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("browser_matrix.unavailable_title"),
		Description: view.Locale.Text("browser_matrix.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("browser_matrix.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
