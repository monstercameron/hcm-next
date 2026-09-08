package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// caseStatusPage is the route adapter for safe
// participant case status. The governed help service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — case truth stays
// server authority. The live status composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func caseStatusPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("case_status.unavailable_title"),
		Description: view.Locale.Text("case_status.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("case_status.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
