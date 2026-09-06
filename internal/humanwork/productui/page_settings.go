package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func settingsPage(view View) ui.Node {
	locale := localePreferencesProps(view)
	accessibility := AccessibilityPreferencesProps{
		I18nProps: I18nProps{Locale: view.Locale}, Value: view.Accessibility,
		TextSizes: AccessibilityTextSizeOptions(), Contrasts: AccessibilityContrastOptions(),
		Motions: AccessibilityMotionOptions(), LinkStyles: AccessibilityLinkOptions(),
		OnPreview: view.PreviewAccessibility, OnSave: view.SaveAccessibility, OnReset: view.ResetAccessibility,
	}
	return ui.CreateElement(SettingsPage, SettingsPageProps{
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
	})
}
