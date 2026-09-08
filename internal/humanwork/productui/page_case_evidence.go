package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// caseEvidencePage is the route adapter for restricted
// case evidence review. The governed help service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that reviews nothing — evidence truth
// stays server authority. The live review composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func caseEvidencePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("case_evidence.unavailable_title"),
		Description: view.Locale.Text("case_evidence.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("case_evidence.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
