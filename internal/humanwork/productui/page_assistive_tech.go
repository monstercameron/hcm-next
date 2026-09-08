package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// assistiveTechPage is the route adapter for
// assistive-technology compatibility. The governed
// qualification service is not published to this UI yet,
// so the surface keeps the journeys fallback contract:
// an honest empty state with a recovery link that
// qualifies nothing — compatibility truth stays server
// authority. The live compatibility composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func assistiveTechPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("assistive_tech.unavailable_title"),
		Description: view.Locale.Text("assistive_tech.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("assistive_tech.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
