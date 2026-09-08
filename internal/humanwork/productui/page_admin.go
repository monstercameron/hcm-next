package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func adminPage(view View) ui.Node {
	journeyState, journeyTone := "Connected", "positive"
	journeyAvailability := ActionState{}
	if view.LoadError != "" {
		journeyState, journeyTone = "Unavailable", "warning"
		journeyAvailability = ActionState{Availability: ActionUnavailable, Reason: view.Locale.Text("admin.journeys_unavailable_reason")}
	}
	// The studio cell publishes no configuration service: the action is
	// unavailable with its reason and no live link, never a live link
	// to something unavailable.
	studioAvailability := ActionState{Availability: ActionUnavailable, Reason: view.Locale.Text("admin.studio_unavailable_reason")}
	return ui.CreateElement(AdminPage, AdminPageProps{
		Hero: AdminHeroProps{
			Eyebrow: "LIVE CELL", Title: valueOrUnavailable(view.Tenant), Description: "This page reports only services the authenticated cell has actually exposed.",
			Action: ActionLinkProps{Label: "Open Journeys", Href: statefulHref(view, PageJourneys), Class: "button primary", Navigate: view.Navigate},
		},
		Capabilities: []CapabilityCardProps{
			{
				Title: "Roles & access", Description: "Create tenant roles and assign one or more roles to every employee from the live workforce directory.", State: "Available", Tone: "positive",
				Action: ActionLinkProps{Label: "Manage roles →", Href: statefulHref(view, PageRoles), Navigate: view.Navigate},
			},
			{
				Title: "Organization visibility", Description: "For each role, choose everyone, the employee's own unit, an approved set of units, or all units except a restricted set.", State: "Available", Tone: "positive",
				Action: ActionLinkProps{Label: "Configure visibility →", Href: statefulHref(view, PageOrganizationVisibility), Navigate: view.Navigate},
			},
			{
				Title: "Worker ID rules", Description: "Issue organization-specific worker numbers from an atomic, non-reusing sequence with governed formatting rules.", State: "Available", Tone: "positive",
				Action: ActionLinkProps{Label: "Configure worker IDs →", Href: statefulHref(view, PageWorkerIDs), Navigate: view.Navigate},
			},
			{
				Title: "Brand & appearance", Description: "Governed palettes, shapes, density, glyphs, and motion are available across the product shell.", State: "Available", Tone: "positive",
				Action: ActionLinkProps{Label: "Configure appearance →", Href: statefulHref(view, PageAppearance), Navigate: view.Navigate},
			},
			{
				Title: "Journey service", Description: "Promotion journeys and visible workers are loaded through the canonical gRPC service.", State: journeyState, Tone: journeyTone, Availability: journeyAvailability,
				Action: ActionLinkProps{Label: "Open details →", Href: statefulHref(view, PageJourneys), Navigate: view.Navigate},
			},
			{
				Title: "Experience configuration", Description: "No page-configuration service is published by this cell.", State: "Unavailable", Tone: "warning", Availability: studioAvailability,
				Action: ActionLinkProps{Label: "Open details →", Href: statefulHref(view, PageStudio), Navigate: view.Navigate},
			},
		},
	})
}
