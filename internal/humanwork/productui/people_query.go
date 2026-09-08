package productui

import "strings"

// PeopleQuery is the bounded People directory query behind
// the directory page: normalized strings, the typed sort
// contract, a floored page, and an allowlisted page size.
// One builder owns every bound so consumers cannot assemble
// an unbounded query by omission.
type PeopleQuery struct {
	Query     string
	Team      string
	Location  string
	Sort      PeopleSortField
	Direction PeopleSortDirection
	Page      int
	PageSize  int
}

// BuildPeopleQuery composes one bounded directory query from
// raw request parts. Strings arrive trimmed and lowered for
// case-insensitive matching, the sort resolves through the
// typed contract, pages floor at one, and sizes pass the
// directory allowlist.
func BuildPeopleQuery(rawQuery, rawTeam, rawLocation, rawSort, rawDirection string, rawPage, rawPageSize int) PeopleQuery {
	sort, direction := ParsePeopleSort(rawSort, rawDirection)
	page := rawPage
	if page < 1 {
		page = 1
	}
	return PeopleQuery{
		Query:     strings.ToLower(strings.TrimSpace(rawQuery)),
		Team:      strings.ToLower(strings.TrimSpace(rawTeam)),
		Location:  strings.ToLower(strings.TrimSpace(rawLocation)),
		Sort:      sort,
		Direction: direction,
		Page:      page,
		PageSize:  normalizePageSize(rawPageSize),
	}
}
