package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// offboardingEffectsPage is the route adapter for
// offboarding external-effect status. The governed
// lifecycle service is not published to this UI yet, so
// the surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that shows
// nothing — effect truth stays server authority. The
// live status composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func offboardingEffectsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("offboarding_effects.unavailable_title"),
		Description: view.Locale.Text("offboarding_effects.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("offboarding_effects.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
