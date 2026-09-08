package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// timeCorrectionPage is the route adapter for time
// correction. The governed time service is not published
// to this UI yet, so the surface keeps the journeys
// fallback contract: an honest empty state with a
// recovery link that corrects nothing — time truth stays
// server authority. The live correction composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func timeCorrectionPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("time_correction.unavailable_title"),
		Description: view.Locale.Text("time_correction.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("time_correction.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
