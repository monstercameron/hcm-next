package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// headcountPage is the route adapter for headcount
// requests. The authoritative requisition lifecycle
// (RECRUIT-001) is not published yet, so the page keeps
// the journeys fallback contract: an honest empty state
// with a recovery link that exposes no requisitions,
// postings, applications, or candidacies. The live
// requisition composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func headcountPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("headcount.unavailable_title"),
		Description: view.Locale.Text("headcount.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("headcount.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
