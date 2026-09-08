package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// cyclePopulationsPage is the route adapter for
// compensation-cycle populations. The governed
// compensation service is not published to this UI yet,
// so the surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that scopes
// nothing — population truth stays server authority. The
// live populations composition replaces this body once
// the governed service publishes; until then the UI will
// not simulate one.
func cyclePopulationsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("cycle_populations.unavailable_title"),
		Description: view.Locale.Text("cycle_populations.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("cycle_populations.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
