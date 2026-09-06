package productui

import (
	"fmt"
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// peoplePage is the route adapter. It is the only People component that sees
// the broad page projection; every child receives a purpose-built props value.
func peoplePage(view View) ui.Node {
	filtered := filteredPeople(view)
	window := paginatePeople(filtered, view.PeoplePage)
	props := PeoplePageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Summary: PeopleSummaryProps{
			CountLabel: peopleCountLabel(view.Locale, view.Query, len(filtered), len(view.People)),
			ScopeLabel: view.Locale.Text("people.scope", map[string]string{"scope": valueOrUnavailableFor(view.Locale, view.Scope)}),
		},
		Filter: PeopleFilterProps{
			Query: view.Query, Action: pageHref(PagePeople), ClearHref: statefulHref(view, PagePeople),
			NavCollapsed: view.NavCollapsed, Navigate: view.Navigate,
		},
		Empty: PeopleEmptyStateProps{
			ClearHref: statefulHref(view, PagePeople), Navigate: view.Navigate,
		},
	}
	if view.Navigate != nil {
		props.Filter.OnFilter = func(query string) {
			view.Navigate(statefulHref(view, PagePeople, "q", query))
		}
	}
	if window.Total > 0 {
		props.Directory = &PeopleDirectoryProps{
			Rows:       peopleRowProps(view, window),
			Pagination: peoplePaginationProps(view, window),
		}
	}
	return ui.CreateElement(PeoplePage, props)
}

func peopleCountLabel(locale LocaleContext, query string, filteredCount, totalCount int) string {
	if query != "" {
		return locale.Text("people.filtered_count", map[string]string{"filtered": fmt.Sprint(filteredCount), "total": fmt.Sprint(totalCount)})
	}
	return locale.Plural("people.count", int64(totalCount))
}

func peopleRowProps(view View, window peoplePageWindow) []PeopleRowProps {
	rows := make([]PeopleRowProps, 0, len(window.People))
	for _, person := range window.People {
		rows = append(rows, PeopleRowProps{
			Initials: person.Initials, PhotoURL: person.PhotoURL, Name: person.Name, Role: person.Role, Team: person.Team,
			Manager: person.Manager, Location: person.Location, Navigate: view.Navigate,
			Href: statefulHref(view, PagePerson, "person", person.ID, "q", view.Query, "page", peoplePageValue(window.Page)),
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
		Href: statefulHref(view, PagePeople, "q", view.Query, "page", peoplePageValue(page)),
	}
}

func peoplePageValue(page int) string {
	if page <= 1 {
		return ""
	}
	return strconv.Itoa(page)
}
