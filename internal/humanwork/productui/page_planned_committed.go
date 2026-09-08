package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// plannedCommittedPage is the route adapter for the
// planned-versus-committed distinction. The governed
// organization service is not published to this UI yet,
// so the surface keeps the journeys fallback contract:
// an honest empty state with a recovery link that
// distinguishes nothing — plan and commitment truth
// stay server authority. The live distinction
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func plannedCommittedPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("planned_committed.unavailable_title"),
		Description: view.Locale.Text("planned_committed.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("planned_committed.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
