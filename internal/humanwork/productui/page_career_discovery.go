package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// careerDiscoveryPage is the route adapter for
// career-opportunity discovery. The governed growth
// service is not published to this UI yet, so the surface
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that discovers nothing —
// opportunity truth stays server authority. The live
// discovery composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func careerDiscoveryPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("career_discovery.unavailable_title"),
		Description: view.Locale.Text("career_discovery.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("career_discovery.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
