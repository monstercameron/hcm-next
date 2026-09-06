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
	Access      AccessContextProps
	Preferences EmptyStateProps
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
	return html.Div(html.Props{Class: "insights-grid"},
		ui.CreateElement(QuickActions, props.Guidance),
		ui.CreateElement(InformationalPanel, props.Support),
	)
}

func InformationalPanel(props InformationalPanelProps) ui.Node {
	body := html.Div(html.Props{Class: props.Class}, html.P(html.Props{Class: "muted"}, ui.Text(props.Description)))
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})
}

func SettingsPage(props SettingsPageProps) ui.Node {
	return html.Div(html.Props{Class: "insights-grid"},
		ui.CreateElement(AccessContext, props.Access),
		ui.CreateElement(EmptyState, props.Preferences),
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
