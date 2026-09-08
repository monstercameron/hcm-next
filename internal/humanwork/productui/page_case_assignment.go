package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// caseAssignmentPage is the route adapter for case
// assignment and recusal. The governed help service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that assigns nothing — assignment
// truth stays server authority. The live assignment
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func caseAssignmentPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("case_assignment.unavailable_title"),
		Description: view.Locale.Text("case_assignment.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("case_assignment.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
