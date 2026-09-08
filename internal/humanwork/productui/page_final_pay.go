package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// finalPayPage is the route adapter for final-pay and
// benefit status. The governed lifecycle service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that shows nothing — pay truth stays
// server authority. The live status composition replaces
// this body once the governed service publishes; until
// then the UI will not simulate one.
func finalPayPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("final_pay.unavailable_title"),
		Description: view.Locale.Text("final_pay.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("final_pay.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
