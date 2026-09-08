package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// leaveEvidencePage is the route adapter for restricted
// leave-evidence tasks. The governed leave service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that completes nothing — evidence truth
// stays server authority. The live evidence composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func leaveEvidencePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("leave_evidence.unavailable_title"),
		Description: view.Locale.Text("leave_evidence.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("leave_evidence.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
