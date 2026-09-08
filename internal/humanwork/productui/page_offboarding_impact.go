package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// offboardingImpactPage is the route adapter for
// offboarding impact simulation. The governed lifecycle
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that simulates
// nothing — impact truth stays server authority. The
// live simulation composition replaces this body once
// the governed service publishes; until then the UI will
// not simulate one.
func offboardingImpactPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("offboarding_impact.unavailable_title"),
		Description: view.Locale.Text("offboarding_impact.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("offboarding_impact.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
