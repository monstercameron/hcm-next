package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// payBenefitReconPage is the route adapter for payroll
// and benefit reconciliation status. The governed
// reconciliation service is not published to this UI yet,
// so the surface keeps the journeys fallback contract: an
// honest empty state with a recovery link that reports
// nothing — reconciliation truth stays server authority.
// The live status composition replaces this body once the
// governed service publishes; until then the UI will not
// simulate one.
func payBenefitReconPage(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("pay_benefit_recon.unavailable_title"),
		Description: view.Locale.Text("pay_benefit_recon.unavailable_detail"),
		Role:        "status",
		Action: &ActionLinkProps{
			Label: view.Locale.Text("pay_benefit_recon.return_home"), Href: statefulHref(view, PageHome),
			Class:    "button primary",
			Navigate: view.Navigate,
		},
	})
}
