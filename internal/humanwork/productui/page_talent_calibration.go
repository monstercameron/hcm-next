package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// talentCalibrationPage is the route adapter for talent
// calibration. The governed growth service is not
// published to this UI yet, so the surface keeps the
// journeys fallback contract: an honest empty state with
// a recovery link that calibrates nothing — calibration
// truth stays server authority. The live calibration
// composition replaces this body once the governed
// service publishes; until then the UI will not simulate
// one.
func talentCalibrationPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("talent_calibration.unavailable_title"),
		Description: view.Locale.Text("talent_calibration.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("talent_calibration.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
