package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func helpPage(view View) ui.Node {
	return ui.CreateElement(HelpPage, HelpPageProps{
		Guidance: QuickActionsProps{Title: "Popular guidance", Actions: []ActionLinkProps{
			{Label: "Run a promotion review →", Href: statefulHref(view, PageJourneys), Navigate: view.Navigate},
			{Label: "Understand journey stages →", Href: statefulHref(view, PageWork), Navigate: view.Navigate},
			{Label: "Review this session’s access context →", Href: statefulHref(view, PageSettings), Navigate: view.Navigate},
		}},
		Support: InformationalPanelProps{Title: "Support availability", Class: "support-summary", Description: "This cell has not published a support-ticket capability. No form is shown because it could not submit anything."},
	})
}
