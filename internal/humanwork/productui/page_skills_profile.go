package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// skillsProfilePage is the route adapter for the governed
// skills profile. The governed growth service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — skill truth stays
// server authority. The live profile composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func skillsProfilePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("skills_profile.unavailable_title"),
		Description: view.Locale.Text("skills_profile.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("skills_profile.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
