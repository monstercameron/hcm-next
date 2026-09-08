package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// caseMessagingPage is the route adapter for restricted
// case messaging. The governed help service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that messages nothing — message truth
// stays server authority. The live messaging composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func caseMessagingPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("case_messaging.unavailable_title"),
		Description: view.Locale.Text("case_messaging.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("case_messaging.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
