package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func studioPage(view View) ui.Node {
	return ui.CreateElement(StudioPage, StudioPageProps{
		Back: ActionLinkProps{Label: "← Back to Admin", Href: statefulHref(view, PageAdmin), Class: "button secondary", Navigate: view.Navigate},
		State: EmptyStateProps{
			Badge: "Service unavailable", Tone: "warning", Title: "No configuration projection is published",
			Description: "The design components remain available, but this cell does not expose a governed page, brand, navigation, access-policy, or publication service. The UI will not simulate one.",
			Action:      &ActionLinkProps{Label: "Return to live workspace", Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
		},
	})
}
