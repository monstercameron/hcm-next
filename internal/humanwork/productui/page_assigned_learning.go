package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// assignedLearningPage is the route adapter for assigned
// learning. The governed learning service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that assigns nothing — assignment truth
// stays server authority. The live learning composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func assignedLearningPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("assigned_learning.unavailable_title"),
		Description: view.Locale.Text("assigned_learning.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("assigned_learning.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
