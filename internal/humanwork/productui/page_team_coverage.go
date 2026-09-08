package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// teamCoveragePage is the route adapter for team-coverage
// review. The governed time service is not published to
// this UI yet, so the surface keeps the journeys fallback
// contract: an honest empty state with a recovery link
// that shows nothing — coverage truth stays server
// authority. The live review composition replaces this
// body once the governed service publishes; until then
// the UI will not simulate one.
func teamCoveragePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("team_coverage.unavailable_title"),
		Description: view.Locale.Text("team_coverage.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("team_coverage.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
