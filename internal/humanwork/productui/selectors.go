package productui

import (
	"strings"
)

const peoplePageSize = 20

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

func exactPerson(view View) (Person, bool) {
	for _, person := range view.People {
		if person.ID == view.SelectedPerson {
			return person, true
		}
	}
	return Person{}, false
}

func filteredPeople(view View) []Person {
	if strings.TrimSpace(view.Query) == "" {
		return view.People
	}
	query := strings.ToLower(strings.TrimSpace(view.Query))
	result := make([]Person, 0, len(view.People))
	for _, person := range view.People {
		searchable := person.Name + " " + person.Role + " " + person.Team + " " + person.Manager + " " + person.Location + " " +
			person.WorkerNumber + " " + person.JobCode + " " + person.Grade + " " + person.PositionID
		if strings.Contains(strings.ToLower(searchable), query) {
			result = append(result, person)
		}
	}
	return result
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
