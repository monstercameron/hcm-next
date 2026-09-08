package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// leaveTimelinePage is the route adapter for the
// leave-status timeline. The governed leave service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — case truth stays
// server authority. The live timeline composition
// replaces this body once the governed service publishes;
// until then the UI will not simulate one.
func leaveTimelinePage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("leave_timeline.unavailable_title"),
		Description: view.Locale.Text("leave_timeline.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("leave_timeline.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
