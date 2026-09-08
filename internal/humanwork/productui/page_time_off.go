package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// timeOffPage is the route adapter for time-off balance
// and calendar. The governed time service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that books nothing — time truth stays
// server authority. The live calendar composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func timeOffPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("time_off.unavailable_title"),
		Description: view.Locale.Text("time_off.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("time_off.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
