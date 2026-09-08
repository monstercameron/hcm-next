package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// candidatePage is the route adapter for one candidate
// record. The governed candidacy lifecycle (RECRUIT-001)
// is not published yet, so the object page keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that exposes no candidate facts,
// resumes, or scores. The live candidate composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func candidatePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("candidate.unavailable_title"),
		Description: view.Locale.Text("candidate.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("candidate.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
