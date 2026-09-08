package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// portalPage is the route adapter for the external
// candidate portal. Candidates outside the workspace
// hold no roles, so the registry admits the portal
// role-less; it is not primary navigation and it
// exposes no workforce surfaces. The governed candidacy
// service is not published yet, so the portal keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that exposes no postings,
// applications, or candidate data. The live portal
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func portalPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("portal.unavailable_title"),
		Description: view.Locale.Text("portal.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("portal.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
