package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type InsightsPageProps struct {
	Metrics   []MetricProps
	Attention AttentionPanelProps
}

type AttentionPanelProps struct {
	Title       string
	CountLabel  string
	CountValue  string
	Description string
	Action      ActionLinkProps
}

func InsightsPage(props InsightsPageProps) ui.Node {
	return html.Div(html.Props{},
		MetricGrid(props.Metrics),
		ui.CreateElement(AttentionPanel, props.Attention),
	)
}

func AttentionPanel(props AttentionPanelProps) ui.Node {
	body := html.Div(html.Props{Class: "coverage-strip"},
		FactList([]FactProps{{Label: props.CountLabel, Value: props.CountValue}}),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		ui.CreateElement(ActionLink, props.Action),
	)
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})
}
