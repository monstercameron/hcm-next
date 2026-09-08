package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-114: the worker employment section. Overview
// (WEB-113) extracted the section pattern, but the
// employment facts still live only in the page adapter's
// closure: nothing resolves the organization fact set
// independently, so a second employment consumer
// reimplements the list by convention. The compiler needs
// the independent constructor over the shared section
// engine — the page's exact employment fact set with the
// page's exact silent/governed behavior.
func TestTodo_WEB_114(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{
		ID: "worker-avery", Team: "Alpha", Manager: "Yolanda", PositionID: "POS-7",
		Location: "SF",
	}

	silent := ResolveWorkerEmployment(locale, person, nil)
	if silent.Title != locale.Text("person.organization") {
		t.Fatalf("employment title = %q", silent.Title)
	}
	if silent.Description != locale.Text("person.organization_detail") {
		t.Fatalf("employment description = %q", silent.Description)
	}
	var names []string
	values := map[string]string{}
	for _, fact := range silent.Facts {
		names = append(names, fact.Name)
		values[fact.Name] = fact.Value
	}
	wantNames := []string{"organization_unit", "manager", "position_id", "work_location", "company", "business_unit", "cost_center", "work_arrangement"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("employment facts = %q", names)
	}
	if values["organization_unit"] != "Alpha" || values["manager"] != "Yolanda" || values["position_id"] != "POS-7" || values["work_location"] != "SF" {
		t.Fatalf("employment values = %+v", values)
	}
	if values["company"] != locale.Text("common.not_reported") {
		t.Fatalf("empty fact reads as %q", values["company"])
	}

	// Governed records hide withheld facts and project the rest.
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"organization_unit": {Effect: PresentationAllow},
			"manager":           {Disposition: FieldHide},
		}},
	}
	governed := ResolveWorkerEmployment(locale, person, verdicts)
	for _, fact := range governed.Facts {
		if fact.Name == "manager" {
			t.Fatal("hidden fact survives")
		}
	}
	if len(governed.Facts) != len(silent.Facts)-1 {
		t.Fatalf("governed facts = %d, want %d", len(governed.Facts), len(silent.Facts)-1)
	}
}

// Golden: employment outcomes over person/verdict pairs.
func TestTodo_WEB_114_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	people := []Person{
		{ID: "w-1", Team: "Alpha", Manager: "Yol"},
		{ID: "w-2", Location: "NYC"},
	}
	verdictSets := []map[string]AuthorizedRecord{
		nil,
		{"w-1": {ID: "w-1", Disclosable: true, Fields: map[string]AuthorizedField{"manager": {Disposition: FieldHide}}}},
	}
	var builder strings.Builder
	for _, verdicts := range verdictSets {
		for _, person := range people {
			section := ResolveWorkerEmployment(locale, person, verdicts)
			for _, fact := range section.Facts {
				fmt.Fprintf(&builder, "%s|%s\x00", fact.Name, fact.Value)
			}
			builder.WriteString("\n")
		}
		builder.WriteString("==\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "6024933f903edd5747a32412bec76e5a97d9f63e584fbb9a37b0b1ea4d4b8c31"
	if got != want {
		t.Fatalf("employment digest = %s, want %s", got, want)
	}
}

// Browser: employment over person patterns resolves
// deterministically without mutating the record.
func TestTodo_WEB_114_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	patterns := []Person{
		{ID: "w-1", Team: "T1", PositionID: "P1"},
		{ID: "w-2"},
	}
	for _, person := range patterns {
		before := person
		first := ResolveWorkerEmployment(locale, person, nil)
		second := ResolveWorkerEmployment(locale, person, nil)
		if !reflect.DeepEqual(person, before) {
			t.Fatal("employment resolution mutates its record")
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("employment resolution is nondeterministic")
		}
		if len(first.Facts) != 8 {
			t.Fatalf("silent employment holds %d facts", len(first.Facts))
		}
	}
}

// Conformance: overview and employment share one engine —
// identical silent/governed behavior, disjoint fact sets.
func TestTodo_WEB_114_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "w-3", Team: "", WorkerNumber: "WN-3"}
	overview := ResolveWorkerOverview(locale, person, nil)
	employment := ResolveWorkerEmployment(locale, person, nil)
	seen := map[string]bool{}
	for _, fact := range overview.Facts {
		seen[fact.Name] = true
	}
	for _, fact := range employment.Facts {
		if seen[fact.Name] {
			t.Fatalf("fact %q in both sections", fact.Name)
		}
		if fact.Value == "" {
			t.Fatalf("empty value escapes unavailable: %+v", fact)
		}
	}
	again := ResolveWorkerEmployment(locale, person, nil)
	if !reflect.DeepEqual(employment, again) {
		t.Fatal("employment resolution is unstable")
	}
}
