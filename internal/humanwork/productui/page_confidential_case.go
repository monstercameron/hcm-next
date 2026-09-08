package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// confidentialCasePage is the route adapter for
// confidential case intake. The governed help service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that intakes nothing — case truth
// stays server authority. The live intake composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func confidentialCasePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("confidential_case.unavailable_title"),
		Description: view.Locale.Text("confidential_case.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("confidential_case.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
