package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// timeOffRequestPage is the route adapter for the
// time-off request journey. The governed time service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that requests nothing — request truth
// stays server authority. The live journey composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func timeOffRequestPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("time_off_request.unavailable_title"),
		Description: view.Locale.Text("time_off_request.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("time_off_request.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
