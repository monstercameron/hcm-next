package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type WorkPageProps struct {
	I18nProps
	Collection WorkCollectionProps
	Preview    WorkPreviewProps
}

type WorkCollectionProps struct {
	I18nProps
	Title      string
	CountLabel string
	Tabs       []WorkTabProps
	Rows       []WorkRowProps
	Footer     WorkCollectionFooterProps
}

type WorkTabProps struct {
	Label    string
	Href     string
	Active   bool
	Navigate func(string)
}

type WorkRowProps struct {
	I18nProps
	ID               string
	Initials         string
	PhotoURL         string
	Title            string
	Person           string
	Summary          string
	Due              string
	StatusProjection StatusProjection
	Href             string
	Selected         bool
	Navigate         func(string)
}

type WorkCollectionFooterProps struct {
	Label  string
	Action ActionLinkProps
}

type WorkPreviewProps struct {
	I18nProps
	Empty            bool
	ID               string
	Initials         string
	PhotoURL         string
	Title            string
	Person           string
	Summary          string
	StatusProjection StatusProjection
	FactsTitle       string
	Facts            []FactProps
	Action           ActionLinkProps
	EmptyTitle       string
	EmptyDetail      string
}

func WorkPage(props WorkPageProps) ui.Node {
	props.Collection.I18nProps = props.I18nProps
	props.Preview.I18nProps = props.I18nProps
	return html.Div(html.Props{Class: "workbench"},
		ui.CreateElement(WorkCollection, props.Collection),
		ui.CreateElement(WorkPreview, props.Preview),
	)
}

func WorkCollection(props WorkCollectionProps) ui.Node {
	tabs := make([]ui.Node, 0, len(props.Tabs))
	for _, item := range props.Tabs {
		tabs = append(tabs, ui.CreateElement(WorkTab, item))
	}
	rows := make([]ui.Node, 0, len(props.Rows))
	for _, item := range props.Rows {
		item.I18nProps = props.I18nProps
		rows = append(rows, ui.CreateElement(WorkRow, item))
	}
	if len(rows) == 0 {
		rows = append(rows, html.Li(html.Props{Class: "collection-empty"},
			html.Strong(html.Props{}, ui.Text(props.Text("work.empty_title"))),
			html.Small(html.Props{}, ui.Text(props.Text("work.empty_detail"))),
		))
	}
	foot := []ui.Node{html.Span(html.Props{}, ui.Text(props.Footer.Label))}
	if props.Footer.Action.Href != "" {
		foot = append(foot, ui.CreateElement(ActionLink, props.Footer.Action))
	}
	return html.Section(html.Props{Class: "surface work-list", Aria: map[string]string{"label": props.Text("work.collection_label")}},
		html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{}, ui.Text(props.Title)), html.Span(html.Props{Class: "count"}, ui.Text(props.CountLabel))),
		html.Nav(html.Props{Class: "tabs", Aria: map[string]string{"label": props.Text("work.filter_label")}}, tabs...),
		html.Ul(html.Props{Class: "work-rows", Raw: map[string]any{"role": "list"}}, rows...),
		html.Div(html.Props{Class: "panel-foot"}, foot...),
	)
}

func WorkTab(props WorkTabProps) ui.Node {
	class := "tab"
	action := ActionLinkProps{Label: props.Label, Href: props.Href, Class: class, Navigate: props.Navigate}
	if props.Active {
		class += " active"
		return softwareLink(props.Navigate, html.Props{Class: class, Aria: map[string]string{"current": "page"}}, props.Href, ui.Text(props.Label))
	}
	action.Class = class
	return ui.CreateElement(ActionLink, action)
}

func WorkRow(props WorkRowProps) ui.Node {
	class := "work-row"
	if props.Selected {
		class += " selected"
	}
	linkProps := html.Props{Class: class}
	if props.Selected {
		linkProps.Aria = map[string]string{"current": "true"}
	}
	status := ui.CreateElement(StatusPresentation, StatusPresentationProps{I18nProps: props.I18nProps, IDSeed: "work-row-status-" + props.ID, Projection: props.StatusProjection})
	return html.Li(html.Props{Class: "work-row-item"}, softwareLink(props.Navigate, linkProps, props.Href,
		personAvatar(props.Person, props.Initials, props.PhotoURL, ""),
		html.Span(html.Props{Class: "row-main"}, html.Strong(html.Props{}, ui.Text(props.Title)), html.Small(html.Props{}, ui.Text(props.Person)), html.Small(html.Props{}, ui.Text(props.Summary))),
		html.Span(html.Props{Class: "row-end"}, status, html.Small(html.Props{}, ui.Text(props.Due))),
		html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, ui.Text("›")),
	))
}

func WorkPreview(props WorkPreviewProps) ui.Node {
	if props.Empty {
		return ui.CreateElement(EmptyState, EmptyStateProps{
			Title: props.EmptyTitle, Description: props.EmptyDetail, Class: "work-preview", Action: &props.Action,
		})
	}
	facts := append([]ui.Node{html.H3(html.Props{}, ui.Text(props.FactsTitle))}, factRows(props.Facts)...)
	status := ui.CreateElement(StatusPresentation, StatusPresentationProps{I18nProps: props.I18nProps, IDSeed: "work-preview-status-" + props.ID, Projection: props.StatusProjection})
	return html.Aside(html.Props{Class: "surface work-preview", Aria: map[string]string{"label": props.Text("work.selected_summary")}},
		html.Div(html.Props{Class: "preview-head"},
			personAvatar(props.Person, props.Initials, props.PhotoURL, ""),
			html.Div(html.Props{}, html.Small(html.Props{}, ui.Text(props.Text("work.selected_label"))), html.H2(html.Props{}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(fmt.Sprintf("%s · %s", props.Person, props.Summary)))),
			status,
		),
		html.Div(html.Props{Class: "facts"}, facts...),
		ui.CreateElement(ActionLink, props.Action),
	)
}
