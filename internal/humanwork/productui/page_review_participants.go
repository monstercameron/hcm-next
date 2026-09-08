package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reviewParticipantsPage is the route adapter for
// review-participant visibility. The governed growth
// service is not published to this UI yet, so the surface
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that discloses nothing —
// participant truth stays server authority. The live
// disclosure composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func reviewParticipantsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("review_participants.unavailable_title"),
		Description: view.Locale.Text("review_participants.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("review_participants.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
