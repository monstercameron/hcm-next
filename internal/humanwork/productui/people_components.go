package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PeoplePageProps is the complete, transport-neutral contract of the People
// surface. It contains presentation state only and no service or credential.
type PeoplePageProps struct {
	I18nProps
	Summary   PeopleSummaryProps
	Filter    PeopleFilterProps
	Directory *PeopleDirectoryProps
	Empty     PeopleEmptyStateProps
}

// PeopleSummaryProps contains the two already-formatted directory summary
// labels. Formatting them in the page adapter keeps this component reusable.
type PeopleSummaryProps struct {
	CountLabel string
	ScopeLabel string
}

// PeopleFilterProps is the progressive-enhancement search contract. Action
// and hidden state support ordinary GET submission; OnFilter upgrades it to
// software navigation in the WASM client.
type PeopleFilterProps struct {
	I18nProps
	Query        string
	Team         string
	Location     string
	Teams        []PeopleFilterOption
	Locations    []PeopleFilterOption
	Sort         string
	Direction    string
	Action       string
	ClearHref    string
	NavCollapsed bool
	Navigate     func(string)
	OnFilter     func(string, string, string)
}

type PeopleFilterOption struct {
	Value string
	Label string
}

// PeopleDirectoryProps owns only the rows and pagination it renders.
type PeopleDirectoryProps struct {
	I18nProps
	Rows       []PeopleRowProps
	Columns    []PeopleSortColumnProps
	Pagination PeoplePaginationProps
}

// PeopleTableProps is the table's row collection.
type PeopleTableProps struct {
	I18nProps
	Rows    []PeopleRowProps
	Columns []PeopleSortColumnProps
}

// PeopleSortColumnProps is an address-backed, accessible directory sort.
type PeopleSortColumnProps struct {
	Label      string
	Href       string
	Active     bool
	Descending bool
	Navigate   func(string)
}

// PeopleRowProps exposes only fields visible in a directory row.
type PeopleRowProps struct {
	I18nProps
	Initials     string
	PhotoURL     string
	Name         string
	Role         string
	Team         string
	Manager      string
	Location     string
	Href         string
	QuickActions []PeopleQuickActionProps
	Navigate     func(string)
}

// PeopleQuickActionProps describes one employee-scoped workflow shortcut.
// The destination owns authorization; this contract carries presentation and
// routing data only.
type PeopleQuickActionProps struct {
	Label           string
	AccessibleLabel string
	Href            string
}

// PeoplePaginationProps is a fully resolved page window.
type PeoplePaginationProps struct {
	I18nProps
	First     int
	Last      int
	Total     int
	Page      int
	PageCount int
	Previous  PaginationLinkProps
	Next      PaginationLinkProps
}

// PaginationLinkProps makes disabled state explicit instead of encoding it
// as an empty URL.
type PaginationLinkProps struct {
	Label    string
	Href     string
	Disabled bool
	Navigate func(string)
}

// PeopleEmptyStateProps contains the one recovery action the empty state owns.
type PeopleEmptyStateProps struct {
	I18nProps
	ClearHref string
	Navigate  func(string)
}

// PeoplePage composes the independently testable People components.
func PeoplePage(props PeoplePageProps) ui.Node {
	props.Filter.I18nProps = props.I18nProps
	props.Empty.I18nProps = props.I18nProps
	children := []ui.Node{
		ui.CreateElement(PeopleSummary, props.Summary),
		ui.CreateElement(PeopleFilter, props.Filter),
	}
	if props.Directory == nil {
		children = append(children, ui.CreateElement(PeopleEmptyState, props.Empty))
		return html.Div(html.Props{Class: "people-page"}, children...)
	}
	props.Directory.I18nProps = props.I18nProps
	children = append(children, ui.CreateElement(PeopleDirectory, *props.Directory))
	return html.Div(html.Props{Class: "people-page"}, children...)
}

// PeopleSummary renders the result and authorization-scope labels.
func PeopleSummary(props PeopleSummaryProps) ui.Node {
	return html.Div(html.Props{Class: "directory-tools"},
		html.Div(html.Props{},
			html.Strong(html.Props{}, ui.Text(props.CountLabel)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.ScopeLabel)),
		),
	)
}

// PeopleFilter renders an SSR-safe GET filter with an optional live callback.
func PeopleFilter(props PeopleFilterProps) ui.Node {
	query, team, location := props.Query, props.Team, props.Location
	inputProps := html.Props{
		ID: "people-filter", Name: "q", Value: props.Query,
		Raw: map[string]any{"type": "search", "placeholder": props.Text("people.filter_placeholder"), "aria-label": props.Text("people.filter_aria")},
	}
	formProps := html.Props{Class: "people-filter", Action: props.Action, Method: "get", Raw: map[string]any{"role": "search"}}
	if props.OnFilter != nil {
		inputProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { query = event.GetValue() })
		onFilter := props.OnFilter
		teamProps := html.Props{ID: "people-team-filter", Name: "team", Value: team, Raw: map[string]any{"aria-label": props.Text("people.team_aria")}}
		locationProps := html.Props{ID: "people-location-filter", Name: "location", Value: location, Raw: map[string]any{"aria-label": props.Text("people.location_aria")}}
		teamProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { team = event.GetValue() })
		locationProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { location = event.GetValue() })
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			onFilter(query, team, location)
		})
		return peopleFilterForm(props, inputProps, teamProps, locationProps, formProps)
	}
	return peopleFilterForm(props, inputProps,
		html.Props{ID: "people-team-filter", Name: "team", Value: team, Raw: map[string]any{"aria-label": props.Text("people.team_aria")}},
		html.Props{ID: "people-location-filter", Name: "location", Value: location, Raw: map[string]any{"aria-label": props.Text("people.location_aria")}},
		formProps)
}

func peopleFilterForm(props PeopleFilterProps, inputProps, teamProps, locationProps, formProps html.Props) ui.Node {
	teamOptions := []ui.Node{html.Option(html.Props{Value: "", Selected: props.Team == ""}, ui.Text(props.Text("people.all_teams")))}
	for _, option := range props.Teams {
		teamOptions = append(teamOptions, html.Option(html.Props{Value: option.Value, Selected: props.Team == option.Value}, ui.Text(option.Label)))
	}
	locationOptions := []ui.Node{html.Option(html.Props{Value: "", Selected: props.Location == ""}, ui.Text(props.Text("people.all_locations")))}
	for _, option := range props.Locations {
		locationOptions = append(locationOptions, html.Option(html.Props{Value: option.Value, Selected: props.Location == option.Value}, ui.Text(option.Label)))
	}
	actions := []ui.Node{html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(props.Text("people.filter")))}
	if props.Query != "" || props.Team != "" || props.Location != "" {
		actions = append(actions, softwareLink(props.Navigate, html.Props{Class: "button secondary"}, props.ClearHref, ui.Text(props.Text("people.clear"))))
	}
	children := []ui.Node{
		html.Label(html.Props{For: "people-filter"}, ui.Text(props.Text("people.find"))),
		html.Div(html.Props{Class: "people-filter-control"},
			html.Tag("input", inputProps),
			html.Select(teamProps, teamOptions...),
			html.Select(locationProps, locationOptions...),
			html.Div(html.Props{Class: "people-filter-actions"}, actions...),
		),
	}
	for _, field := range []struct{ name, value string }{{"sort", props.Sort}, {"dir", props.Direction}} {
		if field.value != "" {
			children = append(children, html.Tag("input", html.Props{Name: field.name, Value: field.value, Raw: map[string]any{"type": "hidden"}}))
		}
	}
	if locale := props.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		children = append(children, html.Tag("input", html.Props{Name: "locale", Value: locale.Resolved, Raw: map[string]any{"type": "hidden"}}))
	}
	if props.NavCollapsed {
		children = append(children, html.Tag("input", html.Props{Name: "nav", Value: "collapsed", Raw: map[string]any{"type": "hidden"}}))
	}
	return html.Form(formProps, children...)
}

// PeopleDirectory composes the table and its pager as one bordered surface.
func PeopleDirectory(props PeopleDirectoryProps) ui.Node {
	props.Pagination.I18nProps = props.I18nProps
	return html.Section(html.Props{Class: "surface people-directory"},
		ui.CreateElement(PeopleTable, PeopleTableProps{I18nProps: props.I18nProps, Rows: props.Rows, Columns: props.Columns}),
		ui.CreateElement(PeoplePagination, props.Pagination),
	)
}

// PeopleTable renders the stable directory columns and supplied rows.
func PeopleTable(props PeopleTableProps) ui.Node {
	nodes := make([]ui.Node, 0, len(props.Rows))
	for _, row := range props.Rows {
		row.I18nProps = props.I18nProps
		nodes = append(nodes, ui.CreateElement(PeopleRow, row))
	}
	headings := make([]ui.Node, 0, len(props.Columns))
	for _, column := range props.Columns {
		headings = append(headings, ui.CreateElement(PeopleSortColumn, column))
	}
	headings = append(headings, html.Span(html.Props{Class: "people-action-heading"}, ui.Text(props.Text("people.column.actions"))))
	return html.Div(html.Props{Class: "people-table"},
		html.Div(html.Props{Class: "people-columns", Aria: map[string]string{"label": props.Text("people.sort_by")}},
			append([]ui.Node{html.Span(html.Props{Class: "people-sort-label"}, ui.Text(props.Text("people.sort_by")))}, headings...)...),
		html.Ul(html.Props{Class: "people-rows", Aria: map[string]string{"label": props.Text("people.table_aria")}, Raw: map[string]any{"role": "list"}}, nodes...),
	)
}

// PeopleSortColumn renders one sortable header with its current direction.
func PeopleSortColumn(props PeopleSortColumnProps) ui.Node {
	ariaSort, indicator := "none", ""
	class := "people-sort"
	if props.Active {
		class += " active"
		if props.Descending {
			ariaSort, indicator = "descending", " ↓"
		} else {
			ariaSort, indicator = "ascending", " ↑"
		}
	}
	linkProps := html.Props{Class: class, Raw: map[string]any{"title": props.Label}}
	if ariaSort != "none" {
		linkProps.Aria = map[string]string{"current": "true"}
	}
	return html.Span(html.Props{},
		softwareLink(props.Navigate, linkProps, props.Href, ui.Text(props.Label+indicator)),
	)
}

// PeopleRow is a software-routed, progressively enhanced directory row.
func PeopleRow(props PeopleRowProps) ui.Node {
	actions := make([]ui.Node, 0, len(props.QuickActions))
	for _, action := range props.QuickActions {
		actions = append(actions, softwareLink(props.Navigate, html.Props{
			Class: "button secondary people-row-action",
			Aria:  map[string]string{"label": action.AccessibleLabel},
			Raw:   map[string]any{"title": action.AccessibleLabel},
		}, action.Href, ui.Text(action.Label)))
	}
	return html.Li(html.Props{Class: "people-row-item people-row"},
		softwareLink(props.Navigate, html.Props{Class: "person-cell people-person-link"}, props.Href,
			personAvatar(props.Name, props.Initials, props.PhotoURL, ""), html.Strong(html.Props{}, ui.Text(props.Name))),
		html.Span(html.Props{Class: "people-cell", Data: map[string]string{"label": props.Text("people.column.role")}}, ui.Text(props.Role)),
		html.Span(html.Props{Class: "people-cell", Data: map[string]string{"label": props.Text("people.column.team")}}, ui.Text(props.Team)),
		html.Span(html.Props{Class: "people-cell", Data: map[string]string{"label": props.Text("people.column.manager")}}, ui.Text(props.Manager)),
		html.Span(html.Props{Class: "people-cell", Data: map[string]string{"label": props.Text("people.column.location")}}, ui.Text(props.Location)),
		html.Div(html.Props{Class: "people-row-actions"}, actions...),
	)
}

// PeoplePagination renders the current range and resolved page actions.
func PeoplePagination(props PeoplePaginationProps) ui.Node {
	return html.Nav(html.Props{Class: "people-pager", Aria: map[string]string{"label": props.Text("people.pages")}},
		html.Span(html.Props{Class: "people-range"}, ui.Text(props.Text("people.range", map[string]string{"first": fmt.Sprint(props.First), "last": fmt.Sprint(props.Last), "total": fmt.Sprint(props.Total)}))),
		html.Div(html.Props{Class: "pager-status", Raw: map[string]any{"aria-live": "polite"}},
			html.Span(html.Props{}, ui.Text(props.Text("people.page_count", map[string]string{"page": fmt.Sprint(props.Page), "pages": fmt.Sprint(props.PageCount)}))),
			ui.CreateElement(PaginationLink, props.Previous),
			ui.CreateElement(PaginationLink, props.Next),
		),
	)
}

// PaginationLink renders disabled pages as non-interactive text.
func PaginationLink(props PaginationLinkProps) ui.Node {
	if props.Disabled {
		return html.Span(html.Props{Class: "button secondary disabled", Raw: map[string]any{"aria-disabled": "true"}}, ui.Text(props.Label))
	}
	return softwareLink(props.Navigate, html.Props{Class: "button secondary"}, props.Href, ui.Text(props.Label))
}

// PeopleEmptyState retains a software-routed recovery action.
func PeopleEmptyState(props PeopleEmptyStateProps) ui.Node {
	return html.Section(html.Props{Class: "surface empty-state", Raw: map[string]any{"role": "status"}},
		html.H2(html.Props{}, ui.Text(props.Text("people.empty_title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("people.empty_detail"))),
		softwareLink(props.Navigate, html.Props{Class: "button secondary"}, props.ClearHref, ui.Text(props.Text("people.clear_filter"))),
	)
}
