package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-119: contextual worker action discovery. The
// person page binds launch hrefs to the viewed worker inside
// its view-coupled launcher, so no independent constructor
// discovers which registered workflows are launchable for a
// worker: the first consumer re-implements the binding and
// the query narrowing by convention. The compiler needs the
// pure discovery — every supplied workflow bound to the
// worker with the launcher's exact query rule — so discovery
// resolves today and the launcher can delegate without
// changing behavior.
func TestTodo_WEB_119(t *testing.T) {
	person := Person{ID: "worker-avery", Name: "Avery Patel"}
	registry := []PersonWorkflow{
		{ID: "wf-time", Name: "Request time off", Category: "Time", Description: "Submit a leave request", Href: "/workflows/time", LaunchHref: func(id string) string { return "/workers/" + id + "/time" }},
		{ID: "wf-expense", Name: "File expense", Category: "Money", Description: "Submit an expense report", Href: "/workflows/expense"},
	}

	actions := DiscoverWorkerActions(registry, person, "")
	if len(actions) != 2 {
		t.Fatalf("blank query discovers %d actions, want 2", len(actions))
	}
	if actions[0].Href != "/workers/worker-avery/time" {
		t.Fatalf("launch href = %q, want worker-bound href", actions[0].Href)
	}
	if actions[1].Href != "/workflows/expense" {
		t.Fatalf("static href = %q", actions[1].Href)
	}
	if actions[0].ID != "wf-time" || actions[0].Name != "Request time off" || actions[0].Category != "Time" || actions[0].Description != "Submit a leave request" {
		t.Fatalf("action drops registry metadata: %+v", actions[0])
	}

	narrowed := DiscoverWorkerActions(registry, person, "  EXPENSE ")
	if len(narrowed) != 1 || narrowed[0].ID != "wf-expense" {
		t.Fatalf("query narrows to %+v", narrowed)
	}
	if missed := DiscoverWorkerActions(registry, person, "zzz"); len(missed) != 0 {
		t.Fatalf("query matches everything: %+v", missed)
	}

	// Discovery never mutates the registry it was given.
	before := []PersonWorkflow{
		{ID: "wf-time", Name: "Request time off", Category: "Time", Description: "Submit a leave request", Href: "/workflows/time", LaunchHref: func(id string) string { return "/workers/" + id + "/time" }},
		{ID: "wf-expense", Name: "File expense", Category: "Money", Description: "Submit an expense report", Href: "/workflows/expense"},
	}
	DiscoverWorkerActions(registry, person, "time")
	for i := range registry {
		if registry[i].ID != before[i].ID || registry[i].Name != before[i].Name || registry[i].Href != before[i].Href || registry[i].UseCount != before[i].UseCount {
			t.Fatal("discovery mutates its registry")
		}
	}
}

// Golden: discovered action IDs and hrefs over
// registry/query/worker triples.
func TestTodo_WEB_119_Golden(t *testing.T) {
	registry := []PersonWorkflow{
		{ID: "wf-a", Name: "Alpha review", Category: "Growth", Description: "Run the review cycle", Href: "/w/a", LaunchHref: func(id string) string { return "/p/" + id + "/a" }},
		{ID: "wf-b", Name: "Beta bonus", Category: "Pay", Description: "Award a bonus", Href: "/w/b"},
	}
	queries := []string{"", "  BETA ", "growth", "zzz"}
	workers := []Person{{ID: "w-1"}, {ID: "w-2"}}
	var builder strings.Builder
	for _, query := range queries {
		for _, worker := range workers {
			for _, action := range DiscoverWorkerActions(registry, worker, query) {
				fmt.Fprintf(&builder, "%s|%s|%s\x00", action.ID, action.Name, action.Href)
			}
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "2b66ca4929a2d9847a0912cfbf379275d6fcd26b5fa5185fde6359a954c590fd"
	if got != want {
		t.Fatalf("action discovery digest = %s, want %s", got, want)
	}
}

// Browser: discovery over registry patterns resolves
// deterministically without retaining the registry.
func TestTodo_WEB_119_Browser(t *testing.T) {
	registries := [][]PersonWorkflow{
		nil,
		{},
		{{ID: "wf-x", Name: "Ex", Category: "C", Description: "D", Href: "/x"}},
	}
	worker := Person{ID: "w-9", Name: "Zed"}
	for _, registry := range registries {
		first := DiscoverWorkerActions(registry, worker, "")
		second := DiscoverWorkerActions(registry, worker, "")
		if !reflect.DeepEqual(first, second) {
			t.Fatal("discovery is nondeterministic")
		}
		if first == nil {
			t.Fatal("discovery returns nil for an empty registry")
		}
	}
}

// Conformance: the query rule matches the launcher —
// trimmed, case-folded, over name/category/description.
func TestTodo_WEB_119_Conformance(t *testing.T) {
	registry := []PersonWorkflow{
		{ID: "wf-a", Name: "Alpha", Category: "Cat", Description: "Desc", Href: "/a"},
	}
	worker := Person{ID: "w-1"}
	for _, query := range []string{"alpha", "ALPHA", "  alpha  ", "cat", "CAT", "desc", "a"} {
		if got := DiscoverWorkerActions(registry, worker, query); len(got) != 1 {
			t.Fatalf("query %q discovers %d actions, want 1", query, len(got))
		}
	}
	if got := DiscoverWorkerActions(registry, worker, "zzz"); len(got) != 0 {
		t.Fatalf("query matches by convention: %+v", got)
	}
	stable := DiscoverWorkerActions(registry, worker, "")
	if !reflect.DeepEqual(stable, DiscoverWorkerActions(registry, worker, "")) {
		t.Fatal("discovery is unstable")
	}
}
