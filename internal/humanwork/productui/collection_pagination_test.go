package productui

import (
	"fmt"
	"strings"
	"testing"
)

func TestPeoplePaginationHonorsSupportedPageSizes(t *testing.T) {
	people := make([]Person, 65)
	for index := range people {
		people[index] = Person{ID: fmt.Sprint(index)}
	}
	window := paginatePeople(people, 2, 10)
	if window.First != 11 || window.Last != 20 || window.PageCount != 7 || len(window.People) != 10 {
		t.Fatalf("unexpected window: %+v", window)
	}
	window = paginatePeople(people, 99, 50)
	if window.Page != 2 || window.First != 51 || window.Last != 65 || len(window.People) != 15 {
		t.Fatalf("clamped window: %+v", window)
	}
}

func TestHistoryAndPeopleRenderSharedPageSizeControls(t *testing.T) {
	people := testView(PagePeople)
	people.PeoplePageSize = 10
	doc, err := Render(people)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="page_size"`, `value="10"`, "selected", "Rows per page", "Workflows"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("people page missing %q", want)
		}
	}

	history := testView(PageHistory)
	history.HistoryPageSize = 10
	doc, err = Render(history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `name="history_page_size"`) {
		t.Fatal("history did not use the shared adjustable page-size control")
	}
}

func TestFrequentWorkflowsSortAheadOfAlphabeticalFallback(t *testing.T) {
	workflows := []PersonWorkflow{{ID: "promotion", Name: "Promotion"}, {ID: "transfer", Name: "Internal transfer"}}
	ranked := rankedPersonWorkflows(workflows, map[string]int64{"promotion": 4})
	if ranked[0].ID != "promotion" || ranked[0].UseCount != 4 {
		t.Fatalf("unexpected ranking: %+v", ranked)
	}
}
