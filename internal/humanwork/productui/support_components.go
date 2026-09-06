package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type HelpPageProps struct {
	Guidance QuickActionsProps
	Support  InformationalPanelProps
}

type InformationalPanelProps struct {
	Title       string
	Description string
	Class       string
}

type SettingsPageProps struct {
	Access        AccessContextProps
	Locale        *LocalePreferencesProps
	Accessibility *AccessibilityPreferencesProps
	Preferences   EmptyStateProps
}

type AccessContextProps struct {
	Title       string
	Description string
	Facts       []FactProps
	Callout     string
}

type StudioPageProps struct {
	Back  ActionLinkProps
	State EmptyStateProps
}

func HelpPage(props HelpPageProps) ui.Node {
	return html.Div(html.Props{Class: "insights-grid settings-accessibility-layout"},
		ui.CreateElement(QuickActions, props.Guidance),
		ui.CreateElement(InformationalPanel, props.Support),
	)
}

func InformationalPanel(props InformationalPanelProps) ui.Node {
	body := html.Div(html.Props{Class: props.Class}, html.P(html.Props{Class: "muted"}, ui.Text(props.Description)))
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})
}

func SettingsPage(props SettingsPageProps) ui.Node {
	preferencePanel := ui.CreateElement(EmptyState, props.Preferences)
	if props.Accessibility != nil {
		preferencePanel = ui.CreateElement(AccessibilityPreferencesPanel, *props.Accessibility)
	}
	overview := []ui.Node{ui.CreateElement(AccessContext, props.Access)}
	if props.Locale != nil {
		overview = append(overview, ui.CreateElement(LocalePreferencesPanel, *props.Locale))
	}
	return html.Div(html.Props{Class: "settings-page-stack"},
		html.Div(html.Props{Class: "settings-overview-grid"}, overview...),
		preferencePanel,
	)
}

func AccessContext(props AccessContextProps) ui.Node {
	return html.Aside(html.Props{Class: "settings-context"},
		html.H2(html.Props{}, ui.Text(props.Title)),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		FactList(props.Facts),
		html.P(html.Props{Class: "callout"}, ui.Text(props.Callout)),
	)
}

func StudioPage(props StudioPageProps) ui.Node {
	return html.Div(html.Props{Class: "studio-page"},
		ui.CreateElement(ActionLink, props.Back),
		ui.CreateElement(EmptyState, props.State),
	)
}
