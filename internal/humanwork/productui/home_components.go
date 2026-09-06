package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type HomePageProps struct {
	Work       WorkCollectionProps
	Overview   SummaryCardProps
	QuickStart QuickActionsProps
	Recent     RecentActivityProps
}

type SummaryCardProps struct {
	Title string
	Facts []FactProps
}

type QuickActionsProps struct {
	Title   string
	Actions []ActionLinkProps
}

type RecentActivityProps struct {
	Title            string
	Items            []ActivityProps
	EmptyTitle       string
	EmptyDescription string
}

func HomePage(props HomePageProps) ui.Node {
	return html.Div(html.Props{},
		html.Div(html.Props{Class: "home-grid"},
			ui.CreateElement(WorkCollection, props.Work),
			html.Div(html.Props{Class: "side-stack"},
				ui.CreateElement(SummaryCard, props.Overview),
				ui.CreateElement(QuickActions, props.QuickStart),
			),
		),
		ui.CreateElement(RecentActivity, props.Recent),
	)
}

func SummaryCard(props SummaryCardProps) ui.Node {
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: FactList(props.Facts)})
}

func QuickActions(props QuickActionsProps) ui.Node {
	actions := make([]ui.Node, 0, len(props.Actions))
	for _, action := range props.Actions {
		actions = append(actions, ui.CreateElement(ActionLink, action))
	}
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: html.Div(html.Props{Class: "quick-actions"}, actions...)})
}

func RecentActivity(props RecentActivityProps) ui.Node {
	return ui.CreateElement(Panel, PanelProps{
		Title: props.Title,
		Body:  ActivityList(props.Items, props.EmptyTitle, props.EmptyDescription),
	})
}
