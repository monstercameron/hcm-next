package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// compProposalsPage is the route adapter for manager
// compensation proposals. The governed compensation
// service is not published to this UI yet, so the surface
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that proposes nothing —
// proposal truth stays server authority. The live
// proposals composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func compProposalsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("comp_proposals.unavailable_title"),
		Description: view.Locale.Text("comp_proposals.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("comp_proposals.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
