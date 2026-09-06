package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type AdminPageProps struct {
	Hero         AdminHeroProps
	Capabilities []CapabilityCardProps
}

type AdminHeroProps struct {
	Eyebrow     string
	Title       string
	Description string
	Action      ActionLinkProps
}

type CapabilityCardProps struct {
	Title       string
	Description string
	State       string
	Tone        string
	Action      ActionLinkProps
}

func AdminPage(props AdminPageProps) ui.Node {
	children := []ui.Node{ui.CreateElement(AdminHero, props.Hero)}
	for _, capability := range props.Capabilities {
		children = append(children, ui.CreateElement(CapabilityCard, capability))
	}
	return html.Div(html.Props{Class: "admin-grid"}, children...)
}

func AdminHero(props AdminHeroProps) ui.Node {
	return html.Section(html.Props{Class: "surface admin-hero"},
		html.Div(html.Props{}, html.Small(html.Props{}, ui.Text(props.Eyebrow)), html.H2(html.Props{}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(props.Description))),
		ui.CreateElement(ActionLink, props.Action),
	)
}

func CapabilityCard(props CapabilityCardProps) ui.Node {
	tone := props.Tone
	if tone == "" {
		tone = "positive"
	}
	return html.Section(html.Props{Class: "surface admin-card"},
		html.Div(html.Props{}, html.H3(html.Props{}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(props.Description))),
		html.Strong(html.Props{Class: tone}, ui.Text(props.State)),
		ui.CreateElement(ActionLink, props.Action),
	)
}
