package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ActionLinkProps is the shared navigation contract used by feature
// components. Navigate upgrades same-application links to software navigation;
// cross-application links remain progressively enhanced browser links.
type ActionLinkProps struct {
	Label    string
	Href     string
	Class    string
	Navigate func(string)
}

// FactProps is an already-formatted label/value pair.
type FactProps struct {
	Label string
	Value string
}

// MetricProps is a single operational measure and its provenance note.
type MetricProps struct {
	Label string
	Value string
	Note  string
}

// ActivityProps is one compact timeline entry.
type ActivityProps struct {
	Title  string
	Detail string
	When   string
}

// PanelProps is the common titled-surface composition primitive. Body is a
// deliberate slot: low-level layout accepts children while feature props stay
// transport-neutral and contain presentation data only.
type PanelProps struct {
	Title string
	Class string
	Body  ui.Node
}

// EmptyStateProps standardizes honest empty and unavailable states.
type EmptyStateProps struct {
	Title       string
	Description string
	Badge       string
	Tone        string
	Class       string
	Role        string
	Action      *ActionLinkProps
}

func ActionLink(props ActionLinkProps) ui.Node {
	return softwareLink(props.Navigate, html.Props{Class: props.Class}, props.Href, ui.Text(props.Label))
}

func Panel(props PanelProps) ui.Node {
	class := "surface panel"
	if props.Class != "" {
		class += " " + props.Class
	}
	return html.Section(html.Props{Class: class},
		html.Div(html.Props{Class: "section-head"}, html.H2(html.Props{}, ui.Text(props.Title))),
		props.Body,
	)
}

func EmptyState(props EmptyStateProps) ui.Node {
	class := "surface empty-state"
	if props.Class != "" {
		class += " " + props.Class
	}
	raw := map[string]any{}
	if props.Role != "" {
		raw["role"] = props.Role
	}
	children := make([]ui.Node, 0, 4)
	if props.Badge != "" {
		tone := props.Tone
		if tone == "" {
			tone = "warning"
		}
		children = append(children, html.Span(html.Props{Class: "status " + tone}, ui.Text(props.Badge)))
	}
	children = append(children,
		html.H2(html.Props{}, ui.Text(props.Title)),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
	)
	if props.Action != nil {
		children = append(children, ui.CreateElement(ActionLink, *props.Action))
	}
	return html.Section(html.Props{Class: class, Raw: raw}, children...)
}

func FactList(facts []FactProps) ui.Node {
	return html.Div(html.Props{Class: "facts"}, factRows(facts)...)
}

func factRows(facts []FactProps) []ui.Node {
	children := make([]ui.Node, 0, len(facts))
	for _, item := range facts {
		children = append(children, html.Div(html.Props{},
			html.Span(html.Props{}, ui.Text(item.Label)),
			html.Strong(html.Props{}, ui.Text(item.Value)),
		))
	}
	return children
}

func MetricGrid(metrics []MetricProps) ui.Node {
	children := make([]ui.Node, 0, len(metrics))
	for _, item := range metrics {
		children = append(children, html.Div(html.Props{Class: "metric"},
			html.Span(html.Props{Class: "muted"}, ui.Text(item.Label)),
			html.Strong(html.Props{}, ui.Text(item.Value)),
			html.Small(html.Props{}, ui.Text(item.Note)),
		))
	}
	return html.Div(html.Props{Class: "metrics"}, children...)
}

func ActivityList(items []ActivityProps, emptyTitle, emptyDescription string) ui.Node {
	children := make([]ui.Node, 0, len(items))
	for _, item := range items {
		children = append(children, html.Div(html.Props{Class: "activity"},
			html.Span(html.Props{Class: "check"}, ui.Text("✓")),
			html.Span(html.Props{Class: "row-main"}, html.Strong(html.Props{}, ui.Text(item.Title)), html.Small(html.Props{}, ui.Text(item.Detail))),
			html.Small(html.Props{}, ui.Text(item.When)),
		))
	}
	if len(children) == 0 {
		children = append(children, html.Div(html.Props{Class: "collection-empty"},
			html.Strong(html.Props{}, ui.Text(emptyTitle)),
			html.Small(html.Props{}, ui.Text(emptyDescription)),
		))
	}
	return html.Div(html.Props{Class: "recent"}, children...)
}
