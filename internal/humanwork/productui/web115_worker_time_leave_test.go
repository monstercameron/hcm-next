package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-115: the worker time and leave section. The
// spec resolves it independently, but no constructor exists:
// the worker record carries no time/leave facts and no
// section owns the empty state, so the first time consumer
// invents balances or headers by convention. The compiler
// needs the independent constructor — the spec'd title with
// the record's (empty) time fact set over the shared engine
// — so the section resolves today and fills when the
// authorized time record projects facts.
func TestTodo_WEB_115(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "worker-avery", Name: "Avery Patel"}

	silent := ResolveWorkerTimeLeave(locale, person, nil)
	if silent.Title != locale.Text("person.time_leave") {
		t.Fatalf("time/leave title = %q", silent.Title)
	}
	if silent.Description != locale.Text("person.time_leave_detail") {
		t.Fatalf("time/leave description = %q", silent.Description)
	}
	if len(silent.Facts) != 0 {
		t.Fatalf("record invents time facts: %+v", silent.Facts)
	}

	// Governed verdicts conjure no facts either.
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow},
		}},
	}
	if governed := ResolveWorkerTimeLeave(locale, person, verdicts); len(governed.Facts) != 0 {
		t.Fatalf("verdicts conjure time facts: %+v", governed.Facts)
	}
}

// Golden: time/leave outcomes over person/verdict pairs.
func TestTodo_WEB_115_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	people := []Person{
		{ID: "w-1", Name: "Amy"},
		{ID: "w-2", HireDate: "2021-06-01"},
	}
	verdictSets := []map[string]AuthorizedRecord{
		nil,
		{"w-1": {ID: "w-1", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationAllow}}}},
	}
	var builder strings.Builder
	for _, verdicts := range verdictSets {
		for _, person := range people {
			section := ResolveWorkerTimeLeave(locale, person, verdicts)
			fmt.Fprintf(&builder, "%s|%d\x00", section.Title, len(section.Facts))
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "0576cc644022342fa81a94247a62be867fa804487c9d741993cd8af089bd8cdc"
	if got != want {
		t.Fatalf("time/leave digest = %s, want %s", got, want)
	}
}

// Browser: time/leave over person patterns resolves
// deterministically without mutating the record.
func TestTodo_WEB_115_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	patterns := []Person{
		{ID: "w-1", Name: "Ann"},
		{ID: "w-2"},
	}
	for _, person := range patterns {
		before := person
		first := ResolveWorkerTimeLeave(locale, person, nil)
		second := ResolveWorkerTimeLeave(locale, person, nil)
		if !reflect.DeepEqual(person, before) {
			t.Fatal("time/leave resolution mutates its record")
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("time/leave resolution is nondeterministic")
		}
		if len(first.Facts) != 0 {
			t.Fatalf("record invents time facts: %+v", first.Facts)
		}
	}
}

// Conformance: the section shares the engine — same title
// mechanics, disjoint (empty) facts; stability.
func TestTodo_WEB_115_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "w-3", Name: "Ned"}
	section := ResolveWorkerTimeLeave(locale, person, nil)
	if section.Title == "" || section.Description == "" {
		t.Fatalf("section untitled: %+v", section)
	}
	if section.Title == ResolveWorkerOverview(locale, person, nil).Title {
		t.Fatal("time/leave reuses another section title")
	}
	again := ResolveWorkerTimeLeave(locale, person, nil)
	if !reflect.DeepEqual(section, again) {
		t.Fatal("time/leave resolution is unstable")
	}
}
