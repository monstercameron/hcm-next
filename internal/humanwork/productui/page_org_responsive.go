package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// orgResponsivePage is the route adapter for responsive
// organization exploration. The governed organization
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that explores
// nothing — org truth stays server authority. The live
// responsive composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func orgResponsivePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("org_responsive.unavailable_title"),
		Description: view.Locale.Text("org_responsive.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("org_responsive.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
