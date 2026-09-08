package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// onboardingPage is the route adapter for onboarding
// plans. The governed onboarding service is not
// published to this UI yet, so the page keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that exposes no tasks, owners, or due
// dates. The live onboarding composition replaces this
// body once the governed service publishes; until then
// the UI will not simulate one.
func onboardingPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("onboarding.unavailable_title"),
		Description: view.Locale.Text("onboarding.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("onboarding.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
