package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// disasterRecoveryPage is the route adapter for frontend
// disaster recovery. The governed recovery service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that proves nothing — recovery truth
// stays server authority. The live recovery composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func disasterRecoveryPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("disaster_recovery.unavailable_title"),
		Description: view.Locale.Text("disaster_recovery.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("disaster_recovery.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
