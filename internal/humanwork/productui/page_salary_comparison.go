package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// salaryComparisonPage is the route adapter for
// salary-range and budget comparison. The governed
// compensation service is not published to this UI yet,
// so the surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that compares
// nothing — band truth stays server authority. The live
// comparison composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func salaryComparisonPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("salary_comparison.unavailable_title"),
		Description: view.Locale.Text("salary_comparison.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("salary_comparison.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
