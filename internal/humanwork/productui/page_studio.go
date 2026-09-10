package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func studioPage(view View) ui.Node {
	return ui.CreateElement(StudioPage, StudioPageProps{
		Back: ActionLinkProps{Label: "← Back to Admin", Href: statefulHref(view, PageAdmin), Class: "studio-back-link", Navigate: view.Navigate},
		State: EmptyStateProps{
			Badge: "Not available", Tone: "warning", Title: "Custom page editing is not enabled",
			Description: "This workspace does not have a governed page-builder service enabled. You can still customize Brand & appearance and manage Roles & access in Admin settings.",
			Action:      &ActionLinkProps{Label: "Return to live workspace", Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
		},
	})
}
