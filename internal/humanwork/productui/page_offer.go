package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// offerPage is the route adapter for offer review and
// acceptance. The governed offer service is not
// published to this UI yet, so the page keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that exposes no offer terms and
// accepts nothing — acceptance stays server authority.
// The live offer composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func offerPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("offer.unavailable_title"),
		Description: view.Locale.Text("offer.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("offer.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
