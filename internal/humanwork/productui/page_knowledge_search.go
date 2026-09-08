package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// knowledgeSearchPage is the route adapter for
// authorized knowledge search. The governed help service
// is not published to this UI yet, so the surface keeps
// the journeys fallback contract: an honest empty state
// with a recovery link that searches nothing — knowledge
// truth stays server authority. The live search
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func knowledgeSearchPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("knowledge_search.unavailable_title"),
		Description: view.Locale.Text("knowledge_search.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("knowledge_search.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
