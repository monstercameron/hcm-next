package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// candidatesPage is the route adapter for the candidate
// pipeline. The governed candidacy lifecycle
// (RECRUIT-001) is not published yet, so the pipeline
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that exposes no candidates,
// applications, resumes, or scores. The live pipeline
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func candidatesPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("candidates.unavailable_title"),
		Description: view.Locale.Text("candidates.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("candidates.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
