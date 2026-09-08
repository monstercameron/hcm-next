package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// exitCompletionPage is the route adapter for exit
// completion and correction. The governed lifecycle
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that completes
// nothing — completion truth stays server authority.
// The live completion composition replaces this body
// once the governed service publishes; until then the UI
// will not simulate one.
func exitCompletionPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("exit_completion.unavailable_title"),
		Description: view.Locale.Text("exit_completion.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("exit_completion.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
