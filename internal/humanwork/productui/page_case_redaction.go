package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// caseRedactionPage is the route adapter for case-view
// redaction and audit. The governed help service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that proves nothing — redaction truth
// stays server authority. The live redaction composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func caseRedactionPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("case_redaction.unavailable_title"),
		Description: view.Locale.Text("case_redaction.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("case_redaction.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
