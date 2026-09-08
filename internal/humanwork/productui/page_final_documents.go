package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// finalDocumentsPage is the route adapter for
// final-document delivery. The governed lifecycle
// service is not published to this UI yet, so the
// surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that delivers
// nothing — document truth stays server authority. The
// live delivery composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func finalDocumentsPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("final_documents.unavailable_title"),
		Description: view.Locale.Text("final_documents.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("final_documents.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
