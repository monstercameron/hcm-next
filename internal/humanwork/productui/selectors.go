package productui

import (
	"sort"
	"strings"
)

const defaultPageSize = 20

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

// admittedWork limits work instances to disclosable records once the
// server speaks about work: instances without a verdict and
// non-disclosable instances leave every extractable listing — queue
// rows, history artifacts, and derived counts — so an export drawn
// from these listings can never carry what the viewer may not open.
// Silence is population-scoped: a verdict map addressing only person
// records (as on profile renders) says nothing about journeys, and
// dropping journeys for a verdict about a person would invent
// authority presentation does not have. An empty map, or a map with
// no work-instance verdict, keeps the current set. Order is
// preserved, inputs are never mutated, and an empty result resolves
// to nil.
func admittedWork(view View) []WorkItem {
	if !workVerdictsPresent(view) {
		return view.Work
	}
	admitted := make([]WorkItem, 0, len(view.Work))
	for _, item := range view.Work {
		if DiscoveryAdmitted(item.ID, view.RecordVerdicts) {
			admitted = append(admitted, item)
		}
	}
	if len(admitted) == 0 {
		return nil
	}
	return admitted
}

// peopleVerdictsPresent reports whether the verdict map addresses the
// directory population at all: at least one projected worker carries
// a verdict. Journey verdicts alone leave workers ungated.
func peopleVerdictsPresent(view View) bool {
	if len(view.RecordVerdicts) == 0 {
		return false
	}
	for _, person := range view.People {
		if _, ok := view.RecordVerdicts[person.ID]; ok {
			return true
		}
	}
	return false
}

// workVerdictsPresent reports whether the verdict map addresses the work
// population at all: at least one projected instance carries a verdict.
// Person-record verdicts alone leave journeys ungated.
func workVerdictsPresent(view View) bool {
	if len(view.RecordVerdicts) == 0 {
		return false
	}
	for _, item := range view.Work {
		if _, ok := view.RecordVerdicts[item.ID]; ok {
			return true
		}
	}
	return false
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

// admittedPeople limits the directory population to admitted records
// once the server speaks about people: records without a verdict and
// non-disclosable records leave the population, so rows, facet
// options, and counts can never advertise what the viewer may not
// open. Silence is population-scoped, symmetric with admittedWork: a
// verdict map addressing only journeys says nothing about workers,
// and dropping workers for a verdict about a journey would invent
// authority presentation does not have. An empty map, or a map with
// no person-record verdict, keeps the current population. Order is
// preserved and inputs are never mutated.
func admittedPeople(view View) []Person {
	if !peopleVerdictsPresent(view) {
		return view.People
	}
	admitted := make([]Person, 0, len(view.People))
	for _, person := range view.People {
		if DiscoveryAdmitted(person.ID, view.RecordVerdicts) {
			admitted = append(admitted, person)
		}
	}
	if len(admitted) == 0 {
		return nil
	}
	return admitted
}

func filteredPeople(view View) []Person {
	query := strings.ToLower(strings.TrimSpace(view.Query))
	team := strings.ToLower(strings.TrimSpace(view.PeopleTeam))
	location := strings.ToLower(strings.TrimSpace(view.PeopleLocation))
	population := admittedPeople(view)
	if query == "" && team == "" && location == "" {
		return population
	}
	result := make([]Person, 0, len(population))
	for _, person := range population {
		index := normalizedPerson(person)
		if query != "" && !strings.Contains(index.search, query) {
			continue
		}
		if team != "" && index.team != team {
			continue
		}
		if location != "" && index.location != location {
			continue
		}
		result = append(result, person)
	}
	return result
}

func sortedPeople(people []Person, field, direction string) []Person {
	parsedField, parsedDirection := ParsePeopleSort(field, direction)
	return SortPeopleDirectory(people, parsedField, parsedDirection)
}

func sortPeopleValues(people []Person, field string, descending bool) []Person {
	type sortablePerson struct {
		index   int
		primary string
		name    string
		id      string
	}
	decorated := make([]sortablePerson, len(people))
	for index, person := range people {
		normalized := normalizedPerson(person)
		decorated[index] = sortablePerson{
			index: index, primary: normalizedPeopleSortValue(normalized, field),
			name: normalized.name, id: person.ID,
		}
	}
	sort.SliceStable(decorated, func(left, right int) bool {
		comparison := compareSortText(decorated[left].primary, decorated[right].primary, descending)
		if comparison == 0 {
			// Secondary ordering is always ascending so equal roles, teams, and
			// locations do not visually jump when the primary direction flips.
			comparison = compareSortText(decorated[left].name, decorated[right].name, false)
		}
		if comparison == 0 {
			comparison = strings.Compare(decorated[left].id, decorated[right].id)
		}
		return comparison < 0
	})
	result := make([]Person, len(decorated))
	for index := range decorated {
		result[index] = people[decorated[index].index]
	}
	return result
}

// IndexPeople populates immutable normalized fields once per server
// projection. It mutates only private presentation metadata and preserves all
// public record values and ordering.
func IndexPeople(people []Person) {
	for index := range people {
		people[index].normalized = buildPersonNormalizedIndex(people[index])
	}
}

func normalizedPerson(person Person) personNormalizedIndex {
	if person.normalized.ready {
		return person.normalized
	}
	return buildPersonNormalizedIndex(person)
}

func buildPersonNormalizedIndex(person Person) personNormalizedIndex {
	return personNormalizedIndex{
		ready: true,
		search: strings.ToLower(strings.Join([]string{
			person.Name, person.Role, person.Team, person.Manager, person.Location,
			person.WorkerNumber, person.JobCode, person.Grade, person.PositionID,
		}, " ")),
		name: normalizedSortText(person.Name), role: normalizedSortText(person.Role),
		team: normalizedSortText(person.Team), manager: normalizedSortText(person.Manager),
		location: normalizedSortText(person.Location),
	}
}

func normalizedPeopleSortValue(index personNormalizedIndex, field string) string {
	switch field {
	case peopleSortRole:
		return index.role
	case peopleSortTeam:
		return index.team
	case peopleSortManager:
		return index.manager
	case peopleSortLocation:
		return index.location
	default:
		return index.name
	}
}

func normalizedSortText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// compareSortText keeps unavailable values at the end in both directions.
func compareSortText(left, right string, descending bool) int {
	if left == "" && right != "" {
		return 1
	}
	if left != "" && right == "" {
		return -1
	}
	comparison := strings.Compare(left, right)
	if descending {
		return -comparison
	}
	return comparison
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

func normalizePageSize(value int) int {
	switch value {
	case 10, 20, 50, 100:
		return value
	default:
		return defaultPageSize
	}
}

func paginatePeople(people []Person, requestedPage, requestedPageSize int) peoplePageWindow {
	pageSize := normalizePageSize(requestedPageSize)
	total := len(people)
	pageCount := (total + pageSize - 1) / pageSize
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
	start := (page - 1) * pageSize
	end := start + pageSize
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
