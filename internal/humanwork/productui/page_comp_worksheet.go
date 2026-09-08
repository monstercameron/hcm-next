package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// compWorksheetPage is the route adapter for the
// compensation worksheet. The governed compensation
// service is not published to this UI yet, so the surface
// keeps the journeys fallback contract: an honest empty
// state with a recovery link that shows nothing —
// worksheet truth stays server authority. The live
// worksheet composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func compWorksheetPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("comp_worksheet.unavailable_title"),
		Description: view.Locale.Text("comp_worksheet.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("comp_worksheet.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
