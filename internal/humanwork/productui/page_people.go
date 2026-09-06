package productui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// peoplePage is the route adapter. It is the only People component that sees
// the broad page projection; every child receives a purpose-built props value.
func peoplePage(view View) ui.Node {
	filtered := filteredPeople(view)
	ordered := sortedPeople(filtered, view.PeopleSort, view.PeopleDirection)
	window := paginatePeople(ordered, view.PeoplePage)
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
			Action: pageHref(PagePeople), ClearHref: peopleDirectoryHref(view, 1, "", "", "", view.PeopleSort, view.PeopleDirection),
			NavCollapsed: view.NavCollapsed, Navigate: view.Navigate,
		},
		Empty: PeopleEmptyStateProps{
			ClearHref: peopleDirectoryHref(view, 1, "", "", "", view.PeopleSort, view.PeopleDirection), Navigate: view.Navigate,
		},
	}
	if view.Navigate != nil {
		props.Filter.OnFilter = func(query, team, location string) {
			view.Navigate(peopleDirectoryHref(view, 1, strings.TrimSpace(query), strings.TrimSpace(team), strings.TrimSpace(location), view.PeopleSort, view.PeopleDirection))
		}
	}
	if window.Total > 0 {
		props.Directory = &PeopleDirectoryProps{
			Rows:       peopleRowProps(view, window),
			Columns:    peopleSortColumns(view),
			Pagination: peoplePaginationProps(view, window),
		}
	}
	return ui.CreateElement(PeoplePage, props)
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
			Label: column.label, Active: active == column.field, Descending: active == column.field && direction == peopleSortDescending,
			Href: peopleDirectoryHref(view, 1, view.Query, view.PeopleTeam, view.PeopleLocation, column.field, nextDirection), Navigate: view.Navigate,
		})
	}
	return result
}

func peopleRowProps(view View, window peoplePageWindow) []PeopleRowProps {
	rows := make([]PeopleRowProps, 0, len(window.People))
	for _, person := range window.People {
		actions := []PeopleQuickActionProps{{
			Label:           view.Locale.Text("people.promote"),
			AccessibleLabel: view.Locale.Text("people.promote_aria", map[string]string{"name": person.Name}),
			Href:            JourneyProposalHref(view, person.ID),
		}}
		rows = append(rows, PeopleRowProps{
			Initials: person.Initials, PhotoURL: person.PhotoURL, Name: person.Name, Role: person.Role, Team: person.Team,
			Manager: person.Manager, Location: person.Location, Navigate: view.Navigate,
			Href: peoplePersonHref(view, person.ID, window.Page), QuickActions: actions,
		})
	}
	return rows
}

func peoplePaginationProps(view View, window peoplePageWindow) PeoplePaginationProps {
	return PeoplePaginationProps{
		First: window.First, Last: window.Last, Total: window.Total, Page: window.Page, PageCount: window.PageCount,
		Previous: paginationLinkProps(view, view.Locale.Text("common.previous"), window.Page-1, window.Page <= 1),
		Next:     paginationLinkProps(view, view.Locale.Text("common.next"), window.Page+1, window.Page >= window.PageCount),
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
		"sort", sortField, "dir", direction, "page", peoplePageValue(page))
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
		"sort", sortField, "dir", direction, "page", peoplePageValue(page))
}

func peoplePageValue(page int) string {
	if page <= 1 {
		return ""
	}
	return strconv.Itoa(page)
}
