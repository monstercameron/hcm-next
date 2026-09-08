package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// onboardingTasksPage is the route adapter for onboarding
// task completion. The governed onboarding service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that completes nothing — completion
// stays server authority. The live task composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func onboardingTasksPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("onboarding_tasks.unavailable_title"),
		Description: view.Locale.Text("onboarding_tasks.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("onboarding_tasks.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
