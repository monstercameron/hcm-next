package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-118: the worker documents and activity
// sections. The spec resolves both independently, but no
// constructors exist: the worker record carries no document
// or activity facts, so the first consumer invents entries
// or headers by convention. The compiler needs both
// independent constructors — the spec'd titles with the
// record's (empty) fact sets over the shared engine — so
// the sections resolve today and fill when authorized
// records project facts.
func TestTodo_WEB_118(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "worker-avery", Name: "Avery Patel"}

	documents := ResolveWorkerDocuments(locale, person, nil)
	if documents.Title != locale.Text("person.documents") {
		t.Fatalf("documents title = %q", documents.Title)
	}
	if documents.Description != locale.Text("person.documents_detail") {
		t.Fatalf("documents description = %q", documents.Description)
	}
	if len(documents.Facts) != 0 {
		t.Fatalf("record invents documents: %+v", documents.Facts)
	}

	activity := ResolveWorkerActivity(locale, person, nil)
	if activity.Title != locale.Text("person.activity") {
		t.Fatalf("activity title = %q", activity.Title)
	}
	if activity.Description != locale.Text("person.activity_detail") {
		t.Fatalf("activity description = %q", activity.Description)
	}
	if len(activity.Facts) != 0 {
		t.Fatalf("record invents activity: %+v", activity.Facts)
	}

	// Governed verdicts conjure no facts either.
	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow},
		}},
	}
	if governed := ResolveWorkerDocuments(locale, person, verdicts); len(governed.Facts) != 0 {
		t.Fatalf("verdicts conjure documents: %+v", governed.Facts)
	}
	if governed := ResolveWorkerActivity(locale, person, verdicts); len(governed.Facts) != 0 {
		t.Fatalf("verdicts conjure activity: %+v", governed.Facts)
	}
}

// Golden: documents and activity outcomes over
// person/verdict pairs.
func TestTodo_WEB_118_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	people := []Person{
		{ID: "w-1", Name: "Amy"},
		{ID: "w-2"},
	}
	verdictSets := []map[string]AuthorizedRecord{
		nil,
		{"w-1": {ID: "w-1", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationAllow}}}},
	}
	var builder strings.Builder
	for _, verdicts := range verdictSets {
		for _, person := range people {
			documents := ResolveWorkerDocuments(locale, person, verdicts)
			activity := ResolveWorkerActivity(locale, person, verdicts)
			fmt.Fprintf(&builder, "%s|%d|%s|%d\x00", documents.Title, len(documents.Facts), activity.Title, len(activity.Facts))
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "514f4efb182501c6887a0b33833f1fe621c45a45792b2d9e95a4b3d2b8ed07eb"
	if got != want {
		t.Fatalf("documents/activity digest = %s, want %s", got, want)
	}
}

// Browser: both sections over person patterns resolve
// deterministically without mutating the record.
func TestTodo_WEB_118_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	patterns := []Person{
		{ID: "w-1", Name: "Ann"},
		{ID: "w-2"},
	}
	for _, person := range patterns {
		before := person
		firstDocuments := ResolveWorkerDocuments(locale, person, nil)
		secondDocuments := ResolveWorkerDocuments(locale, person, nil)
		firstActivity := ResolveWorkerActivity(locale, person, nil)
		secondActivity := ResolveWorkerActivity(locale, person, nil)
		if !reflect.DeepEqual(person, before) {
			t.Fatal("section resolution mutates its record")
		}
		if !reflect.DeepEqual(firstDocuments, secondDocuments) || !reflect.DeepEqual(firstActivity, secondActivity) {
			t.Fatal("section resolution is nondeterministic")
		}
		if len(firstDocuments.Facts) != 0 || len(firstActivity.Facts) != 0 {
			t.Fatal("record invents section facts")
		}
	}
}

// Conformance: both sections share the engine with distinct
// titles; stability.
func TestTodo_WEB_118_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "w-3", Name: "Ned"}
	documents := ResolveWorkerDocuments(locale, person, nil)
	activity := ResolveWorkerActivity(locale, person, nil)
	if documents.Title == "" || activity.Title == "" {
		t.Fatal("section untitled")
	}
	for _, title := range []string{documents.Title, activity.Title} {
		if title == ResolveWorkerOverview(locale, person, nil).Title || title == ResolveWorkerGrowth(locale, person, nil).Title {
			t.Fatalf("section reuses another title: %q", title)
		}
	}
	if documents.Title == activity.Title {
		t.Fatal("sections share a title")
	}
	if !reflect.DeepEqual(documents, ResolveWorkerDocuments(locale, person, nil)) ||
		!reflect.DeepEqual(activity, ResolveWorkerActivity(locale, person, nil)) {
		t.Fatal("section resolution is unstable")
	}
}
