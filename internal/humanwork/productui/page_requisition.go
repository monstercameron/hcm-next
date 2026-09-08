package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// requisitionPage is the route adapter for the requisition
// workspace. The governed requisition lifecycle
// (RECRUIT-001) is not published yet, so the workspace
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that exposes no
// requisitions, postings, applications, or candidacies.
// The live requisition composition replaces this body
// once the governed service publishes; until then the UI
// will not simulate one.
func requisitionPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("requisition.unavailable_title"),
		Description: view.Locale.Text("requisition.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("requisition.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
