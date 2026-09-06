package productui

import (
	"sort"
	"strings"
)

const peoplePageSize = 20

const (
	peopleSortName       = "name"
	peopleSortRole       = "role"
	peopleSortTeam       = "team"
	peopleSortManager    = "manager"
	peopleSortLocation   = "location"
	peopleSortAscending  = "asc"
	peopleSortDescending = "desc"
)

type peoplePageWindow struct {
	Page      int
	PageCount int
	First     int
	Last      int
	Total     int
	People    []Person
}

func selectedWork(view View) WorkItem {
	for _, item := range view.Work {
		if item.ID == view.SelectedWork {
			return item
		}
	}
	if len(view.Work) > 0 {
		return view.Work[0]
	}
	return WorkItem{}
}

// OpenWorkItems projects the active assignment set used by My Work and its
// navigation/notification counts. Terminal journeys remain discoverable from
// History and must not be counted as work that still needs attention.
func OpenWorkItems(items []WorkItem) []WorkItem {
	open := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if !item.Terminal {
			open = append(open, item)
		}
	}
	return open
}

func exactPerson(view View) (Person, bool) {
	for _, person := range view.People {
		if person.ID == view.SelectedPerson {
			return person, true
		}
	}
	return Person{}, false
}

func filteredPeople(view View) []Person {
	query := strings.ToLower(strings.TrimSpace(view.Query))
	team := strings.ToLower(strings.TrimSpace(view.PeopleTeam))
	location := strings.ToLower(strings.TrimSpace(view.PeopleLocation))
	if query == "" && team == "" && location == "" {
		return view.People
	}
	result := make([]Person, 0, len(view.People))
	for _, person := range view.People {
		searchable := person.Name + " " + person.Role + " " + person.Team + " " + person.Manager + " " + person.Location + " " +
			person.WorkerNumber + " " + person.JobCode + " " + person.Grade + " " + person.PositionID
		if query != "" && !strings.Contains(strings.ToLower(searchable), query) {
			continue
		}
		if team != "" && strings.ToLower(strings.TrimSpace(person.Team)) != team {
			continue
		}
		if location != "" && strings.ToLower(strings.TrimSpace(person.Location)) != location {
			continue
		}
		result = append(result, person)
	}
	return result
}

func sortedPeople(people []Person, field, direction string) []Person {
	result := append([]Person(nil), people...)
	field = normalizePeopleSort(field)
	descending := normalizePeopleDirection(direction) == peopleSortDescending
	sort.SliceStable(result, func(left, right int) bool {
		leftValue := peopleSortValue(result[left], field)
		rightValue := peopleSortValue(result[right], field)
		comparison := strings.Compare(strings.ToLower(leftValue), strings.ToLower(rightValue))
		if comparison == 0 {
			comparison = strings.Compare(strings.ToLower(result[left].Name), strings.ToLower(result[right].Name))
		}
		if comparison == 0 {
			comparison = strings.Compare(result[left].ID, result[right].ID)
		}
		if descending {
			return comparison > 0
		}
		return comparison < 0
	})
	return result
}

func peopleSortValue(person Person, field string) string {
	switch field {
	case peopleSortRole:
		return person.Role
	case peopleSortTeam:
		return person.Team
	case peopleSortManager:
		return person.Manager
	case peopleSortLocation:
		return person.Location
	default:
		return person.Name
	}
}

func normalizePeopleSort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case peopleSortRole, peopleSortTeam, peopleSortManager, peopleSortLocation:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return peopleSortName
	}
}

func normalizePeopleDirection(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), peopleSortDescending) {
		return peopleSortDescending
	}
	return peopleSortAscending
}

func peopleFacetOptions(people []Person, value func(Person) string) []string {
	seen := make(map[string]string)
	for _, person := range people {
		label := strings.TrimSpace(value(person))
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if _, exists := seen[key]; !exists {
			seen[key] = label
		}
	}
	options := make([]string, 0, len(seen))
	for _, label := range seen {
		options = append(options, label)
	}
	sort.Slice(options, func(left, right int) bool { return strings.ToLower(options[left]) < strings.ToLower(options[right]) })
	return options
}

func paginatePeople(people []Person, requestedPage int) peoplePageWindow {
	total := len(people)
	pageCount := (total + peoplePageSize - 1) / peoplePageSize
	if pageCount < 1 {
		pageCount = 1
	}
	page := requestedPage
	if page < 1 {
		page = 1
	}
	if page > pageCount {
		page = pageCount
	}
	start := (page - 1) * peoplePageSize
	end := start + peoplePageSize
	if end > total {
		end = total
	}
	first := 0
	if total > 0 {
		first = start + 1
	}
	return peoplePageWindow{Page: page, PageCount: pageCount, First: first, Last: end, Total: total, People: people[start:end]}
}

func filteredPersonWorkflows(view View) []PersonWorkflow {
	query := strings.ToLower(strings.TrimSpace(view.WorkflowQuery))
	if query == "" {
		return view.PersonWorkflows
	}
	result := make([]PersonWorkflow, 0, len(view.PersonWorkflows))
	for _, workflow := range view.PersonWorkflows {
		searchable := workflow.Name + " " + workflow.Category + " " + workflow.Description
		if strings.Contains(strings.ToLower(searchable), query) {
			result = append(result, workflow)
		}
	}
	return result
}
