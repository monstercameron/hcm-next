package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func settingsPage(view View) ui.Node {
	return ui.CreateElement(SettingsPage, SettingsPageProps{
		Access: AccessContextProps{
			Title: "Access context", Description: "Facts carried by the server-admitted session",
			Facts: []FactProps{
				{Label: "Organization", Value: valueOrUnavailable(view.Tenant)},
				{Label: "Principal", Value: valueOrUnavailable(view.Principal)},
				{Label: "Purpose / scope", Value: valueOrUnavailable(view.Scope)},
				{Label: "Data source", Value: valueOrUnavailable(view.Source)},
			},
			Callout: "These display facts grant no authority; every RPC is authorized again by the server.",
		},
		Preferences: EmptyStateProps{
			Title: "Personal preferences unavailable", Description: "This cell has not published a governed personal-preference service for locale or notifications. Tenant branding is managed separately under Admin → Brand & appearance.",
		},
	})
}
