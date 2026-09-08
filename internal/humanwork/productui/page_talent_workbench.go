package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// talentWorkbenchPage is the route adapter for the manager
// talent workbench. The governed growth service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — talent truth stays
// server authority. The live workbench composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func talentWorkbenchPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("talent_workbench.unavailable_title"),
		Description: view.Locale.Text("talent_workbench.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("talent_workbench.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
