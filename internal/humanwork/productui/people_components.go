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
	ID         string
	Label      string
	Href       string
	Active     bool
	Descending bool
	Navigate   func(string)
}

// PeopleRowProps exposes only fields visible in a directory row.
type PeopleRowProps struct {
	I18nProps
	ID           string
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
	Frequent        bool
}

// PeoplePaginationProps is a fully resolved page window.
type PeoplePaginationProps struct {
	I18nProps
	AriaLabel string
	First     int
	Last      int
	Total     int
	Page      int
	PageCount int
	Previous  PaginationLinkProps
	Next      PaginationLinkProps
	PageSize  PageSizeControlProps
}

// PageSizeControlProps is shared by every pageable product collection.
type PageSizeControlProps struct {
	I18nProps
	Value    int
	Name     string
	Options  []int
	Action   string
	Fields   map[string]string
	OnChange func(int)
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
	if len(props.Columns) == 0 {
		props.Columns = []PeopleSortColumnProps{
			{ID: peopleSortName, Label: props.Text("people.column.person")},
			{ID: peopleSortRole, Label: props.Text("people.column.role")},
			{ID: peopleSortTeam, Label: props.Text("people.column.team")},
			{ID: peopleSortManager, Label: props.Text("people.column.manager")},
			{ID: peopleSortLocation, Label: props.Text("people.column.location")},
		}
	}
	columns := make([]DataTableColumnProps, 0, len(props.Columns)+1)
	for _, column := range props.Columns {
		direction := DataTableUnsorted
		if column.Active {
			direction = DataTableAscending
			if column.Descending {
				direction = DataTableDescending
			}
		}
		columns = append(columns, DataTableColumnProps{ID: column.ID, Label: column.Label, Href: column.Href, Sort: direction, Navigate: column.Navigate})
	}
	columns = append(columns, DataTableColumnProps{ID: "actions", Label: props.Text("people.column.actions"), Class: "people-action-heading", AlignEnd: true})
	rows := make([]DataTableRowProps, 0, len(props.Rows))
	for _, row := range props.Rows {
		row.I18nProps = props.I18nProps
		rows = append(rows, peopleDataTableRow(row))
	}
	return ui.CreateElement(DataTable, DataTableProps{Caption: props.Text("people.table_aria"), AriaLabel: props.Text("people.table_aria"), SortLabel: props.Text("people.sort_by"), Class: "people-table", Columns: columns, Rows: rows})
}

// PeopleSortColumn renders one sortable header with its current direction.
func PeopleSortColumn(props PeopleSortColumnProps) ui.Node {
	direction := DataTableUnsorted
	if props.Active {
		direction = DataTableAscending
		if props.Descending {
			direction = DataTableDescending
		}
	}
	return ui.CreateElement(DataTableColumn, DataTableColumnProps{ID: props.ID, Label: props.Label, Href: props.Href, Sort: direction, Navigate: props.Navigate})
}

// PeopleRow is a software-routed, progressively enhanced directory row.
func PeopleRow(props PeopleRowProps) ui.Node {
	columns := []DataTableColumnProps{
		{ID: peopleSortName, Label: props.Text("people.column.person")},
		{ID: peopleSortRole, Label: props.Text("people.column.role")},
		{ID: peopleSortTeam, Label: props.Text("people.column.team")},
		{ID: peopleSortManager, Label: props.Text("people.column.manager")},
		{ID: peopleSortLocation, Label: props.Text("people.column.location")},
		{ID: "actions", Label: props.Text("people.column.actions"), AlignEnd: true},
	}
	return ui.CreateElement(dataTableRow, dataTableRowRenderProps{Columns: columns, Row: peopleDataTableRow(props)})
}

func peopleDataTableRow(props PeopleRowProps) DataTableRowProps {
	actions := make([]ui.Node, 0, len(props.QuickActions))
	for _, action := range props.QuickActions {
		label := action.Label
		if action.Frequent {
			label += " · " + props.Text("people.frequent")
		}
		actions = append(actions, html.Li(html.Props{}, softwareLink(props.Navigate, html.Props{
			Class: "people-workflow-option",
			Aria:  map[string]string{"label": action.AccessibleLabel},
			Raw:   map[string]any{"title": action.AccessibleLabel},
		}, action.Href, ui.Text(label))))
	}
	workflowMenu := ui.Node(html.Span(html.Props{Class: "muted"}, ui.Text(props.Text("people.no_workflows"))))
	if len(actions) > 0 {
		workflowMenu = ui.CreateElement(TransientPopover, TransientPopoverProps{
			Kind: "people-workflows", Class: "people-workflow-menu", TriggerClass: "button secondary people-row-action",
			Label:   props.Text("people.workflows_aria", map[string]string{"name": props.Name}),
			Trigger: []ui.Node{ui.Text(props.Text("people.workflows"))}, PanelClass: "people-workflow-options",
			Children: []ui.Node{html.Ul(html.Props{Class: "people-workflow-options-list"}, actions...)},
		})
	}
	return DataTableRowProps{ID: props.ID, Class: "people-row-item people-row", Cells: []DataTableCellProps{
		{ColumnID: peopleSortName, RowHeader: true, Children: []ui.Node{softwareLink(props.Navigate, html.Props{Class: "person-cell people-person-link"}, props.Href,
			personAvatar(props.Name, props.Initials, props.PhotoURL, ""), html.Strong(html.Props{}, ui.Text(props.Name)))}},
		{ColumnID: peopleSortRole, Class: "people-cell", Text: props.Role},
		{ColumnID: peopleSortTeam, Class: "people-cell", Text: props.Team},
		{ColumnID: peopleSortManager, Class: "people-cell", Text: props.Manager},
		{ColumnID: peopleSortLocation, Class: "people-cell", Text: props.Location},
		{ColumnID: "actions", Class: "people-row-actions", Children: []ui.Node{workflowMenu}},
	}}
}

// PeoplePagination renders the current range and resolved page actions.
func PeoplePagination(props PeoplePaginationProps) ui.Node {
	props.PageSize.I18nProps = props.I18nProps
	ariaLabel := props.AriaLabel
	if ariaLabel == "" {
		ariaLabel = props.Text("people.pages")
	}
	return html.Nav(html.Props{Class: "people-pager", Aria: map[string]string{"label": ariaLabel}},
		html.Span(html.Props{Class: "people-range"}, ui.Text(props.Text("people.range", map[string]string{"first": fmt.Sprint(props.First), "last": fmt.Sprint(props.Last), "total": fmt.Sprint(props.Total)}))),
		ui.CreateElement(PageSizeControl, props.PageSize),
		html.Div(html.Props{Class: "pager-status", Raw: map[string]any{"aria-live": "polite"}},
			html.Span(html.Props{}, ui.Text(props.Text("people.page_count", map[string]string{"page": fmt.Sprint(props.Page), "pages": fmt.Sprint(props.PageCount)}))),
			ui.CreateElement(PaginationLink, props.Previous),
			ui.CreateElement(PaginationLink, props.Next),
		),
	)
}

func PageSizeControl(props PageSizeControlProps) ui.Node {
	options := make([]ui.Node, 0, len(props.Options))
	class := "page-size-control"
	selectProps := html.Props{Name: props.Name, Value: fmt.Sprint(props.Value), Raw: map[string]any{"aria-label": props.Text("table.page_size_aria")}}
	if props.OnChange != nil {
		class += " enhanced"
		selectProps.OnChange = ui.UseEvent(func(event ui.InputEvent) {
			var selected int
			_, _ = fmt.Sscan(event.GetValue(), &selected)
			props.OnChange(normalizePageSize(selected))
		})
	}
	for _, size := range props.Options {
		options = append(options, html.Option(html.Props{Value: fmt.Sprint(size), Selected: size == props.Value}, ui.Text(fmt.Sprint(size))))
	}
	children := []ui.Node{html.Label(html.Props{}, ui.Text(props.Text("table.rows_per_page")), html.Select(selectProps, options...))}
	for name, value := range props.Fields {
		if value != "" {
			children = append(children, html.Tag("input", html.Props{Name: name, Value: value, Raw: map[string]any{"type": "hidden"}}))
		}
	}
	children = append(children, html.Button(html.Props{Class: "button secondary page-size-apply", Type: "submit"}, ui.Text(props.Text("table.apply_page_size"))))
	return html.Form(html.Props{Class: class, Action: props.Action, Method: "get"}, children...)
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
