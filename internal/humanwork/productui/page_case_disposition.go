package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// caseDispositionPage is the route adapter for case
// finding and disposition. The governed help service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that finds nothing — finding truth
// stays server authority. The live finding composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func caseDispositionPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("case_disposition.unavailable_title"),
		Description: view.Locale.Text("case_disposition.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("case_disposition.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
