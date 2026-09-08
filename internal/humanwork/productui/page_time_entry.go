package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// timeEntryPage is the route adapter for accessible time
// entry. The governed time service is not published to
// this UI yet, so the surface keeps the journeys fallback
// contract: an honest empty state with a recovery link
// that records nothing — time truth stays server
// authority. The live entry composition replaces this
// body once the governed service publishes; until then
// the UI will not simulate one.
func timeEntryPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("time_entry.unavailable_title"),
		Description: view.Locale.Text("time_entry.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("time_entry.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
