package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// orgOutlinePage is the route adapter for the accessible
// organization outline. The governed organization service
// is not published to this UI yet, so the surface keeps
// the journeys fallback contract: an honest empty state
// with a recovery link that outlines nothing — org truth
// stays server authority. The live outline composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func orgOutlinePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("org_outline.unavailable_title"),
		Description: view.Locale.Text("org_outline.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("org_outline.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
