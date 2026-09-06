package productui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	historySortPerson  = "person"
	historySortChange  = "change"
	historySortClosed  = "closed"
	historySortOutcome = "outcome"
)

func historyPage(view View) ui.Node {
	props := workflowHistoryProps(view, "", view.Locale.Text("history.global_title"),
		view.Locale.Text("history.global_detail"), true)
	return ui.CreateElement(WorkflowHistory, props)
}

func workflowHistoryProps(view View, personID, title, description string, withFilter bool) WorkflowHistoryProps {
	universe := historyUniverse(view, personID)
	items := filteredHistory(view, personID)
	rows := make([]WorkflowHistoryItemProps, 0, len(items))
	for _, item := range items {
		personHref := ""
		if personID == "" {
			if id := stablePersonID(view.People, item.PersonRef); id != "" {
				personHref = statefulHref(view, PagePerson, "person", id)
			}
		}
		rows = append(rows, WorkflowHistoryItemProps{
			Type: view.Locale.Text("history.promotion"), Person: item.Person, PersonHref: personHref, Navigate: view.Navigate,
			Initials: item.Initials, PhotoURL: item.PhotoURL, Summary: item.Summary,
			Outcome: item.Status, Tone: item.Tone, EffectiveDate: item.EffectiveDate,
			CompletedAt: item.CompletedAt, SourceLabel: historySourceLabel(item), Href: item.Href,
		})
	}
	props := WorkflowHistoryProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Title:     title, Description: description,
		EmptyText: view.Locale.Text("history.empty_terminal"),
		Items:     rows, TotalCount: len(universe),
	}
	if !withFilter {
		return props
	}

	target := PageHistory
	showPerson := personID == ""
	if personID != "" {
		target = PagePerson
		view.HistoryPerson = ""
	}
	sortKey := effectiveHistorySort(view.HistorySort)
	direction := effectiveHistoryDirection(view.HistoryDirection)
	filter := &WorkflowHistoryFilterProps{
		Query: view.HistoryQuery, Outcome: view.HistoryOutcome, Person: view.HistoryPerson, Year: view.HistoryYear,
		People: historyPeopleOptions(view, universe), Years: historyYearOptions(universe), ShowPerson: showPerson,
		Action: pageHref(target), ClearHref: historyHref(view, target, personID, "", "", "", "", sortKey, direction),
		PersonID: personID, DirectoryQuery: view.Query, DirectoryPage: view.PeoplePage, DirectoryTeam: view.PeopleTeam,
		DirectoryLocation: view.PeopleLocation, DirectorySort: view.PeopleSort, DirectoryDirection: view.PeopleDirection, WorkflowQuery: view.WorkflowQuery,
		Sort: sortKey, Direction: direction, NavCollapsed: view.NavCollapsed, Navigate: view.Navigate,
	}
	if showPerson {
		filter.SearchPlaceholder = view.Locale.Text("history.search_placeholder")
	} else {
		filter.SearchPlaceholder = view.Locale.Text("history.search_person_placeholder")
	}
	if view.Navigate != nil {
		filter.OnFilter = func(query, outcome, selectedPerson, year string) {
			view.Navigate(historyHref(view, target, personID, strings.TrimSpace(query), strings.ToLower(strings.TrimSpace(outcome)),
				strings.TrimSpace(selectedPerson), strings.TrimSpace(year), sortKey, direction))
		}
	}
	props.Filter = filter
	props.Columns = historySortColumns(view, target, personID, sortKey, direction)
	return props
}

func historySourceLabel(item WorkItem) string {
	if item.InstanceID == "" {
		return "Authoritative record"
	}
	if item.InstanceVersion > 0 {
		return fmt.Sprintf("Authoritative · v%d", item.InstanceVersion)
	}
	return "Authoritative"
}

func historyUniverse(view View, personID string) []WorkItem {
	items := make([]WorkItem, 0, len(view.Work))
	for _, item := range view.Work {
		if !item.Terminal {
			continue
		}
		if personID != "" && stablePersonID(view.People, item.PersonRef) != personID {
			continue
		}
		items = append(items, item)
	}
	return items
}

func filteredHistory(view View, personID string) []WorkItem {
	query := strings.ToLower(strings.TrimSpace(view.HistoryQuery))
	outcome := strings.ToLower(strings.TrimSpace(view.HistoryOutcome))
	selectedPerson := strings.TrimSpace(view.HistoryPerson)
	year := strings.TrimSpace(view.HistoryYear)
	items := make([]WorkItem, 0, len(view.Work))
	for _, item := range historyUniverse(view, personID) {
		if selectedPerson != "" && personID == "" && stablePersonID(view.People, item.PersonRef) != selectedPerson {
			continue
		}
		if outcome != "" && strings.ToLower(item.Status) != outcome {
			continue
		}
		if year != "" && historyEffectiveYear(item.EffectiveDate) != year {
			continue
		}
		searchable := strings.ToLower(strings.Join([]string{item.Title, item.Person, item.Summary, item.Status, item.EffectiveDate, item.ID}, " "))
		if query != "" && !strings.Contains(searchable, query) {
			continue
		}
		items = append(items, item)
	}
	sortHistory(items, effectiveHistorySort(view.HistorySort), effectiveHistoryDirection(view.HistoryDirection))
	return items
}

func historyPeopleOptions(view View, items []WorkItem) []HistoryFilterOption {
	labels := map[string]string{}
	for _, item := range items {
		id := stablePersonID(view.People, item.PersonRef)
		if id != "" && item.Person != "" {
			labels[id] = item.Person
		}
	}
	options := make([]HistoryFilterOption, 0, len(labels))
	for value, label := range labels {
		options = append(options, HistoryFilterOption{Value: value, Label: label})
	}
	sort.Slice(options, func(i, j int) bool { return strings.ToLower(options[i].Label) < strings.ToLower(options[j].Label) })
	return options
}

func historyYearOptions(items []WorkItem) []HistoryFilterOption {
	seen := map[string]bool{}
	for _, item := range items {
		if year := historyEffectiveYear(item.EffectiveDate); year != "" {
			seen[year] = true
		}
	}
	years := make([]string, 0, len(seen))
	for year := range seen {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(years)))
	options := make([]HistoryFilterOption, 0, len(years))
	for _, year := range years {
		options = append(options, HistoryFilterOption{Value: year, Label: year})
	}
	return options
}

func historyEffectiveYear(date string) string {
	date = strings.TrimSpace(date)
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

func normalizeHistorySort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case historySortPerson, historySortChange, historySortClosed, historySortOutcome:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeHistoryDirection(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "asc", "desc":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func effectiveHistorySort(value string) string {
	if normalized := normalizeHistorySort(value); normalized != "" {
		return normalized
	}
	return historySortClosed
}

func effectiveHistoryDirection(value string) string {
	if normalized := normalizeHistoryDirection(value); normalized != "" {
		return normalized
	}
	return "desc"
}

func sortHistory(items []WorkItem, sortKey, direction string) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := historySortValue(items[i], sortKey), historySortValue(items[j], sortKey)
		if left == right {
			left, right = items[i].ID, items[j].ID
		}
		if direction == "desc" {
			return left > right
		}
		return left < right
	})
}

func historySortValue(item WorkItem, sortKey string) string {
	switch sortKey {
	case historySortPerson:
		return strings.ToLower(item.Person)
	case historySortChange:
		return strings.ToLower(item.Summary)
	case historySortOutcome:
		return strings.ToLower(item.Status)
	default:
		if stamp, err := time.Parse("2 Jan 2006 · 15:04 MST", item.CompletedAt); err == nil {
			return stamp.UTC().Format(time.RFC3339)
		}
		return item.CompletedAt
	}
}

func historySortColumns(view View, target PageID, personID, sortKey, direction string) []HistorySortColumnProps {
	definitions := []struct {
		key, label string
		sortable   bool
	}{
		{historySortPerson, "Employee", true}, {historySortChange, "Change", true},
		{historySortClosed, "Closed", true}, {historySortOutcome, "Outcome", true},
	}
	if target == PagePerson {
		definitions[0].label = "Workflow"
		definitions[0].sortable = false
	}
	columns := make([]HistorySortColumnProps, 0, len(definitions))
	for _, definition := range definitions {
		nextDirection := "asc"
		if sortKey == definition.key && direction == "asc" {
			nextDirection = "desc"
		}
		columns = append(columns, HistorySortColumnProps{
			Key: definition.key, Label: definition.label,
			Href:   historyHref(view, target, personID, view.HistoryQuery, view.HistoryOutcome, view.HistoryPerson, view.HistoryYear, definition.key, nextDirection),
			Active: sortKey == definition.key, Descending: sortKey == definition.key && direction == "desc",
			Sortable: definition.sortable, Navigate: view.Navigate,
		})
	}
	return columns
}

func historyHref(view View, target PageID, personID, query, outcome, selectedPerson, year, sortKey, direction string) string {
	if target == PagePerson {
		selectedPerson = ""
	}
	values := []string{
		"history_q", query, "outcome", outcome, "history_person", selectedPerson, "history_year", year,
		"history_sort", sortKey, "history_dir", direction,
	}
	if target == PagePerson {
		values = append(values,
			"person", personID, "q", view.Query, "team", view.PeopleTeam, "location", view.PeopleLocation,
			"sort", view.PeopleSort, "dir", view.PeopleDirection, "page", peoplePageValue(view.PeoplePage), "workflow_q", view.WorkflowQuery,
		)
	}
	return statefulHref(view, target, values...)
}
