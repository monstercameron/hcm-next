package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-101: configurable quick actions. The floorplan
// has a quick-actions slot and the launcher owns authorized
// starts, but nothing resolves a viewer's chosen action IDs
// against the available items: callers hand-filter with ad
// hoc unknown-ID behavior, so stale or forged IDs either leak
// through or break the slot. The compiler needs the governed
// resolution — chosen order kept, repeats collapsed to first
// mention, unknown IDs dropped fail-closed — passing items
// through untouched.
func TestTodo_WEB_101(t *testing.T) {
	available := []ActionLauncherItem{
		{ID: "start:journeys", Label: "Journeys"},
		{ID: "start:filing", Label: "Filing"},
		{ID: "start:search", Label: "Search"},
	}
	resolved := ResolveQuickActions(available, []string{"start:search", "start:journeys"})
	if len(resolved) != 2 || resolved[0].ID != "start:search" || resolved[1].ID != "start:journeys" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if len(ResolveQuickActions(available, nil)) != 0 {
		t.Fatal("empty choice resolves actions")
	}
	if len(ResolveQuickActions(nil, []string{"start:search"})) != 0 {
		t.Fatal("empty catalog resolves actions")
	}

	// Unknown IDs drop fail-closed; repeats collapse.
	resolved = ResolveQuickActions(available, []string{"start:nope", "start:filing", "start:filing", "start:ghost"})
	if len(resolved) != 1 || resolved[0].ID != "start:filing" {
		t.Fatalf("unknown/repeat handling = %+v", resolved)
	}
	if !reflect.DeepEqual(resolved[0], available[1]) {
		t.Fatalf("resolution rewrites items: %+v", resolved[0])
	}
}

// Golden: resolution outcomes over catalog/choice pairs.
func TestTodo_WEB_101_Golden(t *testing.T) {
	catalog := []ActionLauncherItem{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	choices := [][]string{
		nil,
		{},
		{"b"},
		{"c", "a"},
		{"z", "b", "b", "y"},
		{"c", "c", "c"},
	}
	var builder strings.Builder
	for _, choice := range choices {
		for _, item := range ResolveQuickActions(catalog, choice) {
			builder.WriteString(item.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "7ec0bf89ff00dcacf8d8b20799ceab061574af55d1d7dbcd0b80f77cc8f8cde4"
	if got != want {
		t.Fatalf("quick-action digest = %s, want %s", got, want)
	}
}

// Browser: resolution over catalog/choice patterns keeps
// chosen order deterministically.
func TestTodo_WEB_101_Browser(t *testing.T) {
	catalog := []ActionLauncherItem{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	patterns := [][]string{
		{"d", "a"},
		{"b", "b", "a", "z"},
		{"z", "y", "x"},
		{"a", "b", "c", "d"},
	}
	for _, choice := range patterns {
		first := ResolveQuickActions(catalog, choice)
		second := ResolveQuickActions(catalog, choice)
		seen := map[string]bool{}
		var want []string
		for _, id := range choice {
			if seen[id] {
				continue
			}
			seen[id] = true
			for _, item := range catalog {
				if item.ID == id {
					want = append(want, id)
				}
			}
		}
		var got []string
		for _, item := range first {
			got = append(got, item.ID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("choice %q resolved to %q", choice, got)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("resolution is nondeterministic")
		}
	}
}

// Conformance: passthrough identity, no aliasing, stability.
func TestTodo_WEB_101_Conformance(t *testing.T) {
	catalog := []ActionLauncherItem{
		{ID: "a", Label: "A", Href: "/a", Keywords: []string{"k"}},
		{ID: "b", Label: "B", Href: "/b"},
	}
	resolved := ResolveQuickActions(catalog, []string{"b", "a"})
	if len(resolved) != 2 || !reflect.DeepEqual(resolved[0], catalog[1]) || !reflect.DeepEqual(resolved[1], catalog[0]) {
		t.Fatalf("resolution rewrites items: %+v", resolved)
	}
	resolved[0].Label = "mutated"
	again := ResolveQuickActions(catalog, []string{"b", "a"})
	if again[0].Label != "B" || catalog[1].Label != "B" {
		t.Fatal("resolution aliases its inputs")
	}
	if !reflect.DeepEqual(ResolveQuickActions(catalog, []string{"a"}), ResolveQuickActions(catalog, []string{"a"})) {
		t.Fatal("resolution is unstable")
	}
}
