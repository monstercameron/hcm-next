package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// WorkflowHistoryProps is the reusable, route-independent contract for a
// tenant-wide or person-scoped history section.
type WorkflowHistoryProps struct {
	I18nProps
	Title         string
	Description   string
	EmptyText     string
	Items         []WorkflowHistoryItemProps
	FilteredCount int
	TotalCount    int
	Filter        *WorkflowHistoryFilterProps
	Columns       []HistorySortColumnProps
	Pagination    *PeoplePaginationProps
}

// WorkflowHistoryFilterProps carries only the address state owned by the
// history search. OnFilter is supplied by the Go/WASM router when mounted.
type WorkflowHistoryFilterProps struct {
	I18nProps
	Query              string
	SearchPlaceholder  string
	Outcome            string
	Person             string
	Year               string
	People             []HistoryFilterOption
	Years              []HistoryFilterOption
	ShowPerson         bool
	Action             string
	ClearHref          string
	PersonID           string
	DirectoryQuery     string
	DirectoryPage      int
	DirectoryPageSize  int
	HistoryPageSize    int
	DirectoryTeam      string
	DirectoryLocation  string
	DirectorySort      string
	DirectoryDirection string
	WorkflowQuery      string
	Sort               string
	Direction          string
	NavCollapsed       bool
	Navigate           func(string)
	OnFilter           func(query, outcome, person, year string)
}

// HistoryFilterOption is one stable value/label pair in a history filter.
type HistoryFilterOption struct {
	Value string
	Label string
}

// HistorySortColumnProps is a software-routed sortable column heading.
type HistorySortColumnProps struct {
	Key        string
	Label      string
	Href       string
	Active     bool
	Descending bool
	Sortable   bool
	Navigate   func(string)
}

// WorkflowHistoryItemProps is one immutable workflow-record summary.
type WorkflowHistoryItemProps struct {
	I18nProps
	Type          string
	Person        string
	PersonHref    string
	Navigate      func(string)
	Initials      string
	PhotoURL      string
	Summary       string
	Outcome       string
	Tone          string
	EffectiveDate string
	CompletedAt   string
	SourceLabel   string
	Href          string
}

// WorkflowHistory renders terminal records with enough context to recognize
// the transaction before opening its complete audit trail.
func WorkflowHistory(props WorkflowHistoryProps) ui.Node {
	rows := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		item.I18nProps = props.I18nProps
		rows = append(rows, ui.CreateElement(WorkflowHistoryItem, item))
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "section-head history-heading"},
			html.Div(html.Props{},
				html.H2(html.Props{ID: "workflow-history-title"}, ui.Text(props.Title)),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
			),
			html.Span(html.Props{Class: "count", Raw: map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"}}, ui.Text(historyCountLabel(props.Locale, props.FilteredCount, props.TotalCount))),
		),
	}
	if props.Filter != nil {
		filter := *props.Filter
		filter.I18nProps = props.I18nProps
		children = append(children, ui.CreateElement(WorkflowHistoryFilter, filter))
	}
	if len(props.Items) > 0 {
		headings := make([]ui.Node, 0, len(props.Columns)+1)
		for _, column := range props.Columns {
			headings = append(headings, ui.CreateElement(HistorySortColumn, column))
		}
		headings = append(headings, html.Span(html.Props{Class: "history-record-heading", Raw: map[string]any{"role": "columnheader"}}, ui.Text(props.Text("history.record"))))
		table := html.Div(html.Props{Class: "history-table", Raw: map[string]any{"role": "table", "aria-labelledby": "workflow-history-title"}},
			html.Div(html.Props{Raw: map[string]any{"role": "rowgroup"}},
				html.Div(html.Props{Class: "history-columns", Raw: map[string]any{"role": "row"}}, headings...),
			),
			html.Div(html.Props{Class: "history-list", Raw: map[string]any{"role": "rowgroup"}}, rows...),
		)
		children = append(children, table)
		if props.Pagination != nil {
			pager := *props.Pagination
			pager.I18nProps = props.I18nProps
			children = append(children, ui.CreateElement(PeoplePagination, pager))
		}
	} else {
		empty := props.EmptyText
		if empty == "" {
			empty = props.Text("history.none_detail")
		}
		children = append(children, html.Div(html.Props{Class: "history-empty", Raw: map[string]any{"role": "status", "aria-atomic": "true"}},
			html.Strong(html.Props{}, ui.Text(props.Text("history.none"))),
			html.P(html.Props{Class: "muted"}, ui.Text(empty)),
		))
	}
	return html.Section(html.Props{Class: "surface workflow-history", Raw: map[string]any{"aria-labelledby": "workflow-history-title"}}, children...)
}

func historyCountLabel(locale LocaleContext, filtered, total int) string {
	if total > filtered {
		return locale.Text("history.filtered_count", map[string]string{"filtered": fmt.Sprint(filtered), "total": fmt.Sprint(total)})
	}
	return locale.Plural("history.count", int64(filtered))
}

// HistorySortColumn renders one address-backed column sort control.
func HistorySortColumn(props HistorySortColumnProps) ui.Node {
	if !props.Sortable {
		return html.Span(html.Props{Raw: map[string]any{"role": "columnheader"}}, ui.Text(props.Label))
	}
	ariaSort := "none"
	indicator := ""
	class := "history-sort"
	if props.Active {
		class += " active"
		if props.Descending {
			ariaSort, indicator = "descending", " ↓"
		} else {
			ariaSort, indicator = "ascending", " ↑"
		}
	}
	return html.Span(html.Props{Raw: map[string]any{"role": "columnheader", "aria-sort": ariaSort}},
		softwareLink(props.Navigate, html.Props{Class: class}, props.Href, ui.Text(props.Label+indicator)),
	)
}

// WorkflowHistoryFilter is progressively enhanced: plain navigation submits
// a GET, while the mounted Go client routes without reloading the document.
func WorkflowHistoryFilter(props WorkflowHistoryFilterProps) ui.Node {
	query, outcome, person, year := props.Query, props.Outcome, props.Person, props.Year
	placeholder := props.SearchPlaceholder
	if placeholder == "" {
		placeholder = props.Text("history.search_placeholder")
	}
	inputProps := html.Props{ID: "history-search", Name: "history_q", Value: query,
		Raw: map[string]any{"type": "search", "placeholder": placeholder, "aria-label": props.Text("history.search_aria")}}
	selectProps := html.Props{ID: "history-outcome", Name: "outcome", Value: outcome,
		Raw: map[string]any{"aria-label": props.Text("history.outcome_aria")}}
	formProps := html.Props{Class: "history-filter", Action: props.Action, Method: "get", Raw: map[string]any{"role": "search"}}
	if props.OnFilter != nil {
		inputProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { query = event.GetValue() })
		selectProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { outcome = event.GetValue() })
		onFilter := props.OnFilter
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			onFilter(query, outcome, person, year)
		})
	}
	personOptions := []ui.Node{html.Option(html.Props{Value: "", Selected: person == ""}, ui.Text(props.Text("history.all_people")))}
	for _, option := range props.People {
		personOptions = append(personOptions, html.Option(html.Props{Value: option.Value, Selected: person == option.Value}, ui.Text(option.Label)))
	}
	yearOptions := []ui.Node{html.Option(html.Props{Value: "", Selected: year == ""}, ui.Text(props.Text("history.any_year")))}
	for _, option := range props.Years {
		yearOptions = append(yearOptions, html.Option(html.Props{Value: option.Value, Selected: year == option.Value}, ui.Text(option.Label)))
	}
	personSelectProps := html.Props{Name: "history_person", Value: person, Raw: map[string]any{"aria-label": props.Text("history.person_aria")}}
	yearSelectProps := html.Props{Name: "history_year", Value: year, Raw: map[string]any{"aria-label": props.Text("history.year_aria")}}
	if props.OnFilter != nil {
		personSelectProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { person = event.GetValue() })
		yearSelectProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { year = event.GetValue() })
	}
	controls := []ui.Node{html.Tag("input", inputProps)}
	if props.ShowPerson {
		controls = append(controls, html.Select(personSelectProps, personOptions...))
	}
	controls = append(controls,
		html.Select(selectProps,
			html.Option(html.Props{Value: "", Selected: outcome == ""}, ui.Text(props.Text("history.all_outcomes"))),
			html.Option(html.Props{Value: "completed", Selected: outcome == "completed"}, ui.Text(props.Text("history.completed"))),
			html.Option(html.Props{Value: "rejected", Selected: outcome == "rejected"}, ui.Text(props.Text("history.rejected"))),
			html.Option(html.Props{Value: "failed", Selected: outcome == "failed"}, ui.Text(props.Text("history.failed"))),
		),
		html.Select(yearSelectProps, yearOptions...),
		html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(props.Text("history.apply"))),
	)
	children := []ui.Node{
		html.Label(html.Props{For: "history-search"}, ui.Text(props.Text("history.find"))),
		html.Div(html.Props{Class: "history-filter-controls"}, controls...),
	}
	if locale := props.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		children = append(children, html.Tag("input", html.Props{Name: "locale", Value: locale.Resolved, Raw: map[string]any{"type": "hidden"}}))
	}
	if props.Query != "" || props.Outcome != "" || props.Person != "" || props.Year != "" {
		children = append(children, softwareLink(props.Navigate, html.Props{Class: "history-clear"}, props.ClearHref, ui.Text(props.Text("history.clear"))))
	}
	for _, field := range []struct{ name, value string }{
		{"person", props.PersonID}, {"q", props.DirectoryQuery}, {"workflow_q", props.WorkflowQuery},
		{"team", props.DirectoryTeam}, {"location", props.DirectoryLocation}, {"sort", props.DirectorySort}, {"dir", props.DirectoryDirection},
		{"history_sort", props.Sort}, {"history_dir", props.Direction},
	} {
		if field.value != "" {
			children = append(children, html.Tag("input", html.Props{Name: field.name, Value: field.value, Raw: map[string]any{"type": "hidden"}}))
		}
	}
	if props.DirectoryPage > 1 {
		children = append(children, html.Tag("input", html.Props{Name: "page", Value: fmt.Sprint(props.DirectoryPage), Raw: map[string]any{"type": "hidden"}}))
	}
	if normalizePageSize(props.DirectoryPageSize) != defaultPageSize {
		children = append(children, html.Tag("input", html.Props{Name: "page_size", Value: fmt.Sprint(normalizePageSize(props.DirectoryPageSize)), Raw: map[string]any{"type": "hidden"}}))
	}
	if normalizePageSize(props.HistoryPageSize) != defaultPageSize {
		children = append(children, html.Tag("input", html.Props{Name: "history_page_size", Value: fmt.Sprint(normalizePageSize(props.HistoryPageSize)), Raw: map[string]any{"type": "hidden"}}))
	}
	if props.NavCollapsed {
		children = append(children, html.Tag("input", html.Props{Name: "nav", Value: "collapsed", Raw: map[string]any{"type": "hidden"}}))
	}
	return html.Form(formProps, children...)
}

// WorkflowHistoryItem renders one record and keeps the journey detail inside
// the product history router.
func WorkflowHistoryItem(props WorkflowHistoryItemProps) ui.Node {
	dates := []ui.Node{
		html.Small(html.Props{}, ui.Text(props.Text("history.closed", map[string]string{"value": valueOrUnavailable(props.CompletedAt)}))),
		html.Small(html.Props{}, ui.Text(props.Text("history.effective", map[string]string{"value": valueOrUnavailable(props.EffectiveDate)}))),
	}
	if props.SourceLabel != "" {
		dates = append(dates, html.Small(html.Props{Class: "history-source"}, ui.Text(props.SourceLabel)))
	}
	return html.Article(html.Props{Class: "history-row", Raw: map[string]any{"role": "row"}},
		html.Div(html.Props{Class: "history-identity", Raw: map[string]any{"role": "cell"}},
			personAvatar(props.Person, props.Initials, props.PhotoURL, ""),
			html.Div(html.Props{Class: "history-record"},
				html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Type)),
				historyPersonName(props),
			),
		),
		html.P(html.Props{Class: "history-change muted", Raw: map[string]any{"role": "cell"}}, ui.Text(props.Summary)),
		html.Div(html.Props{Class: "history-dates", Raw: map[string]any{"role": "cell"}}, dates...),
		html.Span(html.Props{Class: "status " + props.Tone, Raw: map[string]any{"role": "cell"}}, ui.Text(props.Outcome)),
		html.Div(html.Props{Class: "history-action", Raw: map[string]any{"role": "cell"}}, softwareLink(props.Navigate, html.Props{Class: "button secondary"}, props.Href, ui.Text(props.Text("history.open")))),
	)
}

func historyPersonName(props WorkflowHistoryItemProps) ui.Node {
	if props.PersonHref == "" {
		return html.H3(html.Props{}, ui.Text(props.Person))
	}
	return html.H3(html.Props{}, softwareLink(props.Navigate, html.Props{Class: "history-person-link"}, props.PersonHref, ui.Text(props.Person)))
}
