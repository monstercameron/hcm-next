package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-117: the worker growth section. The spec
// resolves it independently, but no constructor exists:
// the worker record carries no growth facts and the
// launchable workflows are view-coupled, so the first
// growth consumer invents ratings or goals by convention.
// The compiler needs the independent constructor — the
// spec'd title with the record's (empty) growth fact set
// over the shared engine — so the section resolves today
// and fills when an authorized growth record projects
// facts.
func TestTodo_WEB_117(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "worker-avery", Name: "Avery Patel", Grade: "G7"}

	silent := ResolveWorkerGrowth(locale, person, nil)
	if silent.Title != locale.Text("person.growth") {
		t.Fatalf("growth title = %q", silent.Title)
	}
	if silent.Description != locale.Text("person.growth_detail") {
		t.Fatalf("growth description = %q", silent.Description)
	}
	if len(silent.Facts) != 0 {
		t.Fatalf("record invents growth facts: %+v", silent.Facts)
	}

	// Governed verdicts conjure no facts either.
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow},
		}},
	}
	if governed := ResolveWorkerGrowth(locale, person, verdicts); len(governed.Facts) != 0 {
		t.Fatalf("verdicts conjure growth facts: %+v", governed.Facts)
	}
}

// Golden: growth outcomes over person/verdict pairs.
func TestTodo_WEB_117_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	people := []Person{
		{ID: "w-1", Name: "Amy", Grade: "G7"},
		{ID: "w-2"},
	}
	verdictSets := []map[string]AuthorizedRecord{
		nil,
		{"w-1": {ID: "w-1", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationAllow}}}},
	}
	var builder strings.Builder
	for _, verdicts := range verdictSets {
		for _, person := range people {
			section := ResolveWorkerGrowth(locale, person, verdicts)
			fmt.Fprintf(&builder, "%s|%d\x00", section.Title, len(section.Facts))
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "330d877a71995fef9c206588c57d645cfdc5b1427aef451d80afa5df85b88bc1"
	if got != want {
		t.Fatalf("growth digest = %s, want %s", got, want)
	}
}

// Browser: growth over person patterns resolves
// deterministically without mutating the record.
func TestTodo_WEB_117_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	patterns := []Person{
		{ID: "w-1", Name: "Ann", Grade: "G1"},
		{ID: "w-2"},
	}
	for _, person := range patterns {
		before := person
		first := ResolveWorkerGrowth(locale, person, nil)
		second := ResolveWorkerGrowth(locale, person, nil)
		if !reflect.DeepEqual(person, before) {
			t.Fatal("growth resolution mutates its record")
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("growth resolution is nondeterministic")
		}
		if len(first.Facts) != 0 {
			t.Fatalf("record invents growth facts: %+v", first.Facts)
		}
	}
}

// Conformance: the section shares the engine — same title
// mechanics, disjoint (empty) facts; stability.
func TestTodo_WEB_117_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "w-3", Name: "Ned"}
	section := ResolveWorkerGrowth(locale, person, nil)
	if section.Title == "" || section.Description == "" {
		t.Fatalf("section untitled: %+v", section)
	}
	if section.Title == ResolveWorkerOverview(locale, person, nil).Title {
		t.Fatal("growth reuses another section title")
	}
	again := ResolveWorkerGrowth(locale, person, nil)
	if !reflect.DeepEqual(section, again) {
		t.Fatal("growth resolution is unstable")
	}
}
