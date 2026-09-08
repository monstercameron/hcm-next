package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// positionOccupancyPage is the route adapter for
// position occupancy presentation. The governed position
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that presents
// nothing — occupancy truth stays server authority. The
// live occupancy composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func positionOccupancyPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("position_occupancy.unavailable_title"),
		Description: view.Locale.Text("position_occupancy.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("position_occupancy.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
