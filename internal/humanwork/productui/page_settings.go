package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/uicomponents"
)

func settingsPage(view View) ui.Node {
	profile := view.Viewer
	if profile.Name == "" {
		profile.Name = view.Principal
	}
	if profile.Initials == "" {
		profile.Initials = uicomponents.Initials(profile.Name)
	}
	locale := localePreferencesProps(view)
	accessibility := AccessibilityPreferencesProps{
		I18nProps: I18nProps{Locale: view.Locale}, Value: view.Accessibility,
		TextSizes: AccessibilityTextSizeOptions(), Contrasts: AccessibilityContrastOptions(),
		Motions: AccessibilityMotionOptions(), LinkStyles: AccessibilityLinkOptions(),
		OnPreview: view.PreviewAccessibility, OnSave: view.SaveAccessibility, OnReset: view.ResetAccessibility,
	}
	props := SettingsPageProps{
		Profile: ViewerProfileProps{
			SectionLabel: view.Locale.Text("settings.profile_title"), Description: view.Locale.Text("settings.profile_description"),
			Name: profile.Name, Initials: profile.Initials, PhotoURL: profile.PhotoURL, Role: valueOrUnavailableFor(view.Locale, profile.Role),
		},
		Locale: &locale,
		Access: AccessContextProps{
			Title: view.Locale.Text("settings.access_title"), Description: view.Locale.Text("settings.access_description"),
			Facts: []FactProps{
				{Label: view.Locale.Text("settings.organization"), Value: valueOrUnavailableFor(view.Locale, view.Tenant)},
				{Label: view.Locale.Text("settings.principal"), Value: valueOrUnavailableFor(view.Locale, view.Principal)},
				{Label: view.Locale.Text("settings.purpose_scope"), Value: valueOrUnavailableFor(view.Locale, view.Scope)},
				{Label: view.Locale.Text("settings.data_source"), Value: valueOrUnavailableFor(view.Locale, view.Source)},
			},
			Callout: view.Locale.Text("settings.access_callout"),
		},
		Accessibility: &accessibility,
	}
	if view.LogoutHref != "" {
		props.SignOut = &ActionLinkProps{Label: "Sign out", Href: view.LogoutHref, Class: "button secondary"}
	}
	return ui.CreateElement(SettingsPage, props)
}
