package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func adminPage(view View) ui.Node {
	journeyState, journeyTone := "Connected", "positive"
	if view.LoadError != "" {
		journeyState, journeyTone = "Unavailable", "warning"
	}
	return ui.CreateElement(AdminPage, AdminPageProps{
		Hero: AdminHeroProps{
			Eyebrow: "LIVE CELL", Title: valueOrUnavailable(view.Tenant), Description: "This page reports only services the authenticated cell has actually exposed.",
			Action: ActionLinkProps{Label: "Open Journeys", Href: statefulHref(view, PageJourneys), Class: "button primary", Navigate: view.Navigate},
		},
		Capabilities: []CapabilityCardProps{
			{
				Title: "Brand & appearance", Description: "Governed palettes, shapes, density, glyphs, and motion are available across the product shell.", State: "Available", Tone: "positive",
				Action: ActionLinkProps{Label: "Configure appearance →", Href: statefulHref(view, PageAppearance), Navigate: view.Navigate},
			},
			{
				Title: "Journey service", Description: "Promotion journeys and visible workers are loaded through the canonical gRPC service.", State: journeyState, Tone: journeyTone,
				Action: ActionLinkProps{Label: "Open details →", Href: statefulHref(view, PageJourneys), Navigate: view.Navigate},
			},
			{
				Title: "Experience configuration", Description: "No page-configuration service is published by this cell.", State: "Unavailable", Tone: "warning",
				Action: ActionLinkProps{Label: "Open details →", Href: statefulHref(view, PageStudio), Navigate: view.Navigate},
			},
		},
	})
}
