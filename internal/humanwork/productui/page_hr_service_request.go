package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// hrServiceRequestPage is the route adapter for HR
// service-request intake. The governed help service is
// not published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that intakes nothing — request truth
// stays server authority. The live intake composition
// replaces this body once the governed service
// publishes; until then the UI will not simulate one.
func hrServiceRequestPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("hr_service_request.unavailable_title"),
		Description: view.Locale.Text("hr_service_request.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("hr_service_request.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
