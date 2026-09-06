package productui

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// peoplePage is the route adapter. It is the only People component that sees
// the broad page projection; every child receives a purpose-built props value.
func peoplePage(view View) ui.Node {
	filtered := filteredPeople(view)
	ordered := sortedPeople(filtered, view.PeopleSort, view.PeopleDirection)
	window := paginatePeople(ordered, view.PeoplePage, view.PeoplePageSize)
	filterActive := view.Query != "" || view.PeopleTeam != "" || view.PeopleLocation != ""
	props := PeoplePageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Summary: PeopleSummaryProps{
			CountLabel: peopleCountLabel(view.Locale, filterActive, len(filtered), len(view.People)),
			ScopeLabel: view.Locale.Text("people.scope", map[string]string{"scope": valueOrUnavailableFor(view.Locale, view.Scope)}),
		},
		Filter: PeopleFilterProps{
			Query: view.Query, Team: view.PeopleTeam, Location: view.PeopleLocation,
			Teams:     peopleFilterOptions(peopleFacetOptions(view.People, func(person Person) string { return person.Team })),
			Locations: peopleFilterOptions(peopleFacetOptions(view.People, func(person Person) string { return person.Location })),
			Sort:      view.PeopleSort, Direction: view.PeopleDirection,
			Action: pageHref(PagePeople), ClearHref: peopleClearHref(view),
			NavCollapsed: view.NavCollapsed, Navigate: view.Navigate,
		},
		Empty: PeopleEmptyStateProps{
			ClearHref: peopleClearHref(view), Navigate: view.Navigate,
		},
	}
	if view.Navigate != nil {
		props.Filter.OnFilter = func(query, team, location string) {
			view.Navigate(peopleDirectoryHref(view, 1, strings.TrimSpace(query), strings.TrimSpace(team), strings.TrimSpace(location), view.PeopleSort, view.PeopleDirection))
		}
	}
	if window.Total > 0 {
		props.Directory = peopleDirectoryProps(view, window)
	}
	return ui.CreateElement(PeoplePage, props)
}

func peopleDirectoryProps(view View, window peoplePageWindow) *PeopleDirectoryProps {
	props := &PeopleDirectoryProps{
		I18nProps:  I18nProps{Locale: view.Locale},
		Rows:       peopleRowProps(view, window),
		Columns:    peopleSortColumns(view),
		Pagination: peoplePaginationProps(view, window),
		Refreshing: view.RefreshingRegion == RefreshRegionPeopleDirectory,
	}
	props.InputKey = peopleDirectoryInputKey(*props)
	if view.UpdatePeopleDirectory == nil {
		return props
	}
	props.CommitSort = view.UpdatePeopleDirectory
	props.ResolveSort = func(field string, descending bool) PeopleDirectoryProps {
		next := view
		next.PeoplePage = 1
		next.PeopleSort = field
		next.PeopleDirection = peopleSortAscending
		if descending {
			next.PeopleDirection = peopleSortDescending
		}
		filtered := filteredPeople(next)
		window := paginatePeople(sortedPeople(filtered, next.PeopleSort, next.PeopleDirection), next.PeoplePage, next.PeoplePageSize)
		return *peopleDirectoryProps(next, window)
	}
	return props
}

// peopleDirectoryInputKey identifies a fresh parent projection without tying
// component state to slice addresses. A local sort keeps its original input
// key; filters, paging, locale changes, or new worker data produce a new key
// and reset the directory from the incoming props.
func peopleDirectoryInputKey(props PeopleDirectoryProps) string {
	hash := fnv.New64a()
	write := func(values ...string) {
		for _, value := range values {
			_, _ = fmt.Fprintf(hash, "%d:%s|", len(value), value)
		}
	}
	write(props.Locale.Resolved, strconv.Itoa(props.Pagination.Page), strconv.Itoa(props.Pagination.PageCount), strconv.Itoa(props.Pagination.PageSize.Value))
	for _, column := range props.Columns {
		write(column.ID, column.Label, column.Href, strconv.FormatBool(column.Active), strconv.FormatBool(column.Descending))
	}
	for _, row := range props.Rows {
		write(row.ID, row.Name, row.Role, row.Team, row.Manager, row.Location, row.PhotoURL, row.Href)
		for _, action := range row.QuickActions {
			write(action.Label, action.AccessibleLabel, action.Href, strconv.FormatBool(action.Frequent))
		}
	}
	return fmt.Sprintf("%x", hash.Sum64())
}

func peopleClearHref(view View) string {
	href := peopleDirectoryHref(view, 1, "", "", "", view.PeopleSort, view.PeopleDirection)
	return withExplicitEmptyQuery(href, "q", "team", "location")
}

func peopleCountLabel(locale LocaleContext, filtered bool, filteredCount, totalCount int) string {
	if filtered {
		return locale.Text("people.filtered_count", map[string]string{"filtered": fmt.Sprint(filteredCount), "total": fmt.Sprint(totalCount)})
	}
	return locale.Plural("people.count", int64(totalCount))
}

func peopleFilterOptions(values []string) []PeopleFilterOption {
	options := make([]PeopleFilterOption, 0, len(values))
	for _, value := range values {
		options = append(options, PeopleFilterOption{Value: value, Label: value})
	}
	return options
}

func peopleSortColumns(view View) []PeopleSortColumnProps {
	active := normalizePeopleSort(view.PeopleSort)
	direction := normalizePeopleDirection(view.PeopleDirection)
	columns := []struct {
		field string
		label string
	}{
		{peopleSortName, view.Locale.Text("people.column.person")},
		{peopleSortRole, view.Locale.Text("people.column.role")},
		{peopleSortTeam, view.Locale.Text("people.column.team")},
		{peopleSortManager, view.Locale.Text("people.column.manager")},
		{peopleSortLocation, view.Locale.Text("people.column.location")},
	}
	result := make([]PeopleSortColumnProps, 0, len(columns))
	for _, column := range columns {
		nextDirection := peopleSortAscending
		if active == column.field && direction == peopleSortAscending {
			nextDirection = peopleSortDescending
		}
		result = append(result, PeopleSortColumnProps{
			ID: column.field, Label: column.label, Active: active == column.field, Descending: active == column.field && direction == peopleSortDescending,
			Href: peopleDirectoryHref(view, 1, view.Query, view.PeopleTeam, view.PeopleLocation, column.field, nextDirection), Navigate: view.Navigate,
		})
	}
	return result
}

func peopleRowProps(view View, window peoplePageWindow) []PeopleRowProps {
	rows := make([]PeopleRowProps, 0, len(window.People))
	for _, person := range window.People {
		actions := make([]PeopleQuickActionProps, 0, len(view.PersonWorkflows))
		workflows := rankedPersonWorkflows(view.PersonWorkflows, view.WorkflowUses)
		if len(view.EffectivePermissions) > 0 && !view.Can(PageJourneys, "create") {
			workflows = nil
		}
		for _, workflow := range workflows {
			href := workflow.Href
			if workflow.LaunchHref != nil {
				href = workflow.LaunchHref(person.ID)
			}
			if href == "" {
				continue
			}
			actions = append(actions, PeopleQuickActionProps{Label: workflow.Name,
				AccessibleLabel: view.Locale.Text("people.workflow_aria", map[string]string{"workflow": workflow.Name, "name": person.Name}),
				Href:            href, Frequent: workflow.UseCount > 0})
		}
		rows = append(rows, PeopleRowProps{
			ID: person.ID, Initials: person.Initials, PhotoURL: person.PhotoURL, Name: person.Name, Role: person.Role, Team: person.Team,
			Manager: person.Manager, Location: person.Location, Navigate: view.Navigate,
			Href: peoplePersonHref(view, person.ID, window.Page), QuickActions: actions,
		})
	}
	return rows
}

func peoplePaginationProps(view View, window peoplePageWindow) PeoplePaginationProps {
	return PeoplePaginationProps{
		AriaLabel: view.Locale.Text("people.pages"),
		First:     window.First, Last: window.Last, Total: window.Total, Page: window.Page, PageCount: window.PageCount,
		Previous: paginationLinkProps(view, view.Locale.Text("common.previous"), window.Page-1, window.Page <= 1),
		Next:     paginationLinkProps(view, view.Locale.Text("common.next"), window.Page+1, window.Page >= window.PageCount),
		PageSize: pageSizeControlProps(view, PagePeople, "page_size", view.PeoplePageSize, map[string]string{
			"q": view.Query, "team": view.PeopleTeam, "location": view.PeopleLocation, "sort": view.PeopleSort, "dir": view.PeopleDirection,
		}),
	}
}

func paginationLinkProps(view View, label string, page int, disabled bool) PaginationLinkProps {
	return PaginationLinkProps{
		Label: label, Disabled: disabled, Navigate: view.Navigate,
		Href: peopleDirectoryHref(view, page, view.Query, view.PeopleTeam, view.PeopleLocation, view.PeopleSort, view.PeopleDirection),
	}
}

func peopleDirectoryHref(view View, page int, query, team, location, sortField, direction string) string {
	sortField = normalizePeopleSort(sortField)
	direction = normalizePeopleDirection(direction)
	if sortField == peopleSortName {
		sortField = ""
	}
	if direction == peopleSortAscending {
		direction = ""
	}
	return statefulHref(view, PagePeople,
		"q", strings.TrimSpace(query), "team", strings.TrimSpace(team), "location", strings.TrimSpace(location),
		"sort", sortField, "dir", direction, "page", peoplePageValue(page), "page_size", pageSizeValue(view.PeoplePageSize))
}

func peoplePersonHref(view View, personID string, page int) string {
	sortField := normalizePeopleSort(view.PeopleSort)
	direction := normalizePeopleDirection(view.PeopleDirection)
	if sortField == peopleSortName {
		sortField = ""
	}
	if direction == peopleSortAscending {
		direction = ""
	}
	return statefulHref(view, PagePerson,
		"person", personID, "q", view.Query, "team", view.PeopleTeam, "location", view.PeopleLocation,
		"sort", sortField, "dir", direction, "page", peoplePageValue(page), "page_size", pageSizeValue(view.PeoplePageSize))
}

func peoplePageValue(page int) string {
	if page <= 1 {
		return ""
	}
	return strconv.Itoa(page)
}

func pageSizeValue(size int) string {
	if normalizePageSize(size) == defaultPageSize {
		return ""
	}
	return strconv.Itoa(normalizePageSize(size))
}

func pageSizeControlProps(view View, page PageID, param string, value int, fields map[string]string) PageSizeControlProps {
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		fields["locale"] = locale.Resolved
	}
	if view.NavCollapsed {
		fields["nav"] = "collapsed"
	}
	return PageSizeControlProps{
		Value: normalizePageSize(value), Name: param, Options: []int{10, 20, 50, 100}, Action: pageHref(page), Fields: fields,
		OnChange: func(size int) {
			if view.Navigate == nil {
				return
			}
			fields[param] = strconv.Itoa(size)
			pairs := make([]string, 0, len(fields)*2)
			for key, item := range fields {
				pairs = append(pairs, key, item)
			}
			view.Navigate(statefulHref(view, page, pairs...))
		},
	}
}

func rankedPersonWorkflows(workflows []PersonWorkflow, usage map[string]int64) []PersonWorkflow {
	result := append([]PersonWorkflow(nil), workflows...)
	for index := range result {
		if usage[result[index].ID] > result[index].UseCount {
			result[index].UseCount = usage[result[index].ID]
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].UseCount == result[right].UseCount {
			return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
		}
		return result[left].UseCount > result[right].UseCount
	})
	return result
}
