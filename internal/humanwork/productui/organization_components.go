package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type OrganizationPageProps struct {
	Title       string
	Description string
	Groups      []OrganizationGroupProps
	Empty       EmptyStateProps
}

type OrganizationGroupProps struct {
	Name  string
	Count int
}

func OrganizationPage(props OrganizationPageProps) ui.Node {
	if len(props.Groups) == 0 {
		return ui.CreateElement(EmptyState, props.Empty)
	}
	groups := make([]ui.Node, 0, len(props.Groups))
	for _, group := range props.Groups {
		groups = append(groups, html.Li(html.Props{Class: "org-node manager"},
			html.Span(html.Props{Class: "row-main"}, html.Strong(html.Props{}, ui.Text(group.Name)), html.Small(html.Props{}, ui.Text(fmt.Sprintf("%d visible workers", group.Count)))),
		))
	}
	body := html.Div(html.Props{Class: "org"},
		html.P(html.Props{Class: "definition"}, ui.Text(props.Description)),
		html.Ul(html.Props{Class: "org-branches", Raw: map[string]any{"role": "list"}}, groups...),
	)
	return ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})
}
