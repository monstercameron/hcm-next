package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type OrganizationPageProps struct {
	Title       string
	Description string
	ViewLabel   string
	FlatAction  ActionLinkProps
	TreeAction  ActionLinkProps
	TreeActive  bool
	Metadata    BusinessMetadataProps
	Groups      []OrganizationGroupProps
	Tree        []OwnershipNodeProps
	Empty       EmptyStateProps
}

// BusinessMetadataProps is the narrow, reusable contract for an authorized
// organization summary. It intentionally carries display facts rather than a
// page View so future company-record services can populate it without changing
// the component composition.
type BusinessMetadataProps struct {
	Title          string
	Description    string
	Items          []BusinessMetadataItemProps
	FootprintLabel string
	Footprint      []string
	Boundary       string
}

type BusinessMetadataItemProps struct {
	Label string
	Value string
}

type OrganizationGroupProps struct {
	Name       string
	Count      int
	CountLabel string
	Members    []OwnershipNodeProps
}

// OwnershipNodeProps is the recursive, presentation-only reporting-line
// contract. Every person has already passed the page's visibility boundary.
type OwnershipNodeProps struct {
	Name, Role, Team, Initials, PhotoURL, Href string
	WorkerNumber, ReportsLabel                 string
	Navigate                                   func(string)
	Reports                                    []OwnershipNodeProps
	Current                                    bool
}

func OrganizationPage(props OrganizationPageProps) ui.Node {
	sections := make([]ui.Node, 0, 2)
	if props.Metadata.Title != "" {
		sections = append(sections, ui.CreateElement(BusinessMetadata, props.Metadata))
	}
	if len(props.Groups) == 0 && len(props.Tree) == 0 {
		sections = append(sections, ui.CreateElement(EmptyState, props.Empty))
		return html.Div(html.Props{Class: "organization-page"}, sections...)
	}
	content := organizationFlat(props.Groups)
	if props.TreeActive {
		content = organizationTree(props.Tree)
	}
	body := html.Div(html.Props{Class: "org"},
		html.Div(html.Props{Class: "organization-view-head"},
			html.P(html.Props{Class: "definition"}, ui.Text(props.Description)),
			html.Div(html.Props{Class: "organization-view-toggle", Raw: map[string]any{"role": "group", "aria-label": props.ViewLabel}},
				organizationViewAction(props.FlatAction, !props.TreeActive),
				organizationViewAction(props.TreeAction, props.TreeActive),
			),
		),
		content,
	)
	// Exploration is the primary task; business context remains available below it.
	sections = append([]ui.Node{ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})}, sections...)
	return html.Div(html.Props{Class: "organization-page"}, sections...)
}

func organizationViewAction(action ActionLinkProps, active bool) ui.Node {
	props := html.Props{Aria: map[string]string{}}
	if active {
		action.Class += " active"
		props.Aria["current"] = "true"
	}
	props.Class = action.Class
	return softwareLink(action.Navigate, props, action.Href, ui.Text(action.Label))
}

func organizationFlat(values []OrganizationGroupProps) ui.Node {
	groups := make([]ui.Node, 0, len(values))
	for _, group := range values {
		members := make([]ui.Node, 0, len(group.Members))
		for _, member := range group.Members {
			members = append(members, html.Li(html.Props{}, organizationPersonCard(member)))
		}
		groups = append(groups, html.Li(html.Props{Class: "organization-unit"},
			html.Details(html.Props{Class: "organization-unit-disclosure"},
				html.Summary(html.Props{Class: "org-node manager"},
					html.Span(html.Props{Class: "organization-unit-glyph", Aria: map[string]string{"hidden": "true"}}, ui.Text("›")),
					html.Span(html.Props{Class: "row-main"}, html.Strong(html.Props{}, ui.Text(group.Name)), html.Small(html.Props{}, ui.Text(group.CountLabel))),
				),
				html.Ul(html.Props{Class: "organization-unit-members", Raw: map[string]any{"role": "list"}}, members...),
			),
		))
	}
	return html.Ul(html.Props{Class: "org-branches", Raw: map[string]any{"role": "list", "data-organization-view": "flat"}}, groups...)
}

func organizationTree(values []OwnershipNodeProps) ui.Node {
	return ui.CreateElement(OrganizationOwnershipTree, OrganizationOwnershipTreeProps{Nodes: values})
}

type OrganizationOwnershipTreeProps struct {
	Nodes []OwnershipNodeProps
}

// OrganizationOwnershipTree is shared by the organization and self-service
// pages so reporting semantics and accessibility cannot drift between them.
func OrganizationOwnershipTree(props OrganizationOwnershipTreeProps) ui.Node {
	nodes := make([]ui.Node, 0, len(props.Nodes))
	for _, value := range props.Nodes {
		nodes = append(nodes, ownershipNode(value))
	}
	return html.Ul(html.Props{Class: "ownership-tree", Raw: map[string]any{"role": "list", "data-organization-view": "tree"}}, nodes...)
}

func ownershipNode(props OwnershipNodeProps) ui.Node {
	children := make([]ui.Node, 0, len(props.Reports))
	for _, report := range props.Reports {
		children = append(children, ownershipNode(report))
	}
	card := organizationPersonCard(props)
	branch := []ui.Node{card}
	if len(children) > 0 {
		label := props.ReportsLabel
		if label == "" {
			label = organizationCountLabel(View{}, "organization.reports_count", len(children))
		}
		branch = append(branch, html.Details(html.Props{Class: "ownership-reports"},
			html.Summary(html.Props{}, ui.Text(label)),
			html.Ul(html.Props{Raw: map[string]any{"role": "list"}}, children...),
		))
	}
	return html.Li(html.Props{}, branch...)
}

func organizationPersonCard(props OwnershipNodeProps) ui.Node {
	class := "ownership-card"
	if props.Current {
		class += " current-person"
	}
	metadata := []ui.Node{html.Strong(html.Props{}, ui.Text(props.Name))}
	for _, value := range []string{props.WorkerNumber, props.Role, props.Team} {
		if value != "" {
			metadata = append(metadata, html.Small(html.Props{}, ui.Text(value)))
		}
	}
	children := []ui.Node{
		personAvatar(props.Name, props.Initials, props.PhotoURL, "small"),
		html.Span(html.Props{Class: "row-main"}, metadata...),
	}
	linkProps := html.Props{Class: class}
	if props.Current {
		linkProps.Raw = map[string]any{"aria-current": "true"}
	}
	if props.Href == "" {
		return html.Div(linkProps, children...)
	}
	return softwareLink(props.Navigate, linkProps, props.Href, children...)
}

func BusinessMetadata(props BusinessMetadataProps) ui.Node {
	items := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		items = append(items, html.Div(html.Props{Class: "business-metadata-item"},
			html.Tag("dt", html.Props{}, ui.Text(item.Label)),
			html.Tag("dd", html.Props{}, ui.Text(item.Value)),
		))
	}
	body := []ui.Node{html.Tag("dl", html.Props{Class: "business-metadata-grid"}, items...)}
	if len(props.Footprint) > 0 {
		locations := make([]ui.Node, 0, len(props.Footprint))
		for _, location := range props.Footprint {
			locations = append(locations, html.Li(html.Props{}, ui.Text(location)))
		}
		body = append(body, html.Div(html.Props{Class: "business-footprint"},
			html.H3(html.Props{}, ui.Text(props.FootprintLabel)),
			html.Ul(html.Props{Raw: map[string]any{"role": "list"}}, locations...),
		))
	}
	if props.Boundary != "" {
		body = append(body, html.P(html.Props{Class: "business-metadata-boundary"}, ui.Text(props.Boundary)))
	}
	return html.Section(html.Props{Class: "surface organization-metadata", Raw: map[string]any{"aria-labelledby": "business-metadata-title"}},
		html.Div(html.Props{Class: "section-head"}, html.Div(html.Props{},
			html.H2(html.Props{ID: "business-metadata-title"}, ui.Text(props.Title)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		)),
		html.Div(html.Props{Class: "business-metadata-body"}, body...),
	)
}
