package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// accessEquipmentPage is the route adapter for access
// and equipment reconciliation. The governed lifecycle
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that
// reconciles nothing — reconciliation truth stays
// server authority. The live reconciliation composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func accessEquipmentPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("access_equipment.unavailable_title"),
		Description: view.Locale.Text("access_equipment.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("access_equipment.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
