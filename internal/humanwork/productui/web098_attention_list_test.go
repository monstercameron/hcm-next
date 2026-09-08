package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-098: the unified attention list. Needs-action work
// scatters across the work collection and ad-hoc filters with no
// single governed list for the attention slot: terminal and
// active items mix by caller convention. The lifecycle needs a
// pure attention filter — non-terminal admitted items in
// admission order. Order stays server-ranked: presentation
// invents no priority, due-ness, or severity without a stated
// rule, and terminal items belong to continuity and history,
// never attention.
func TestTodo_WEB_098(t *testing.T) {
	items := []WorkItem{
		{ID: "a", Title: "Active one"},
		{ID: "b", Title: "Done one", Terminal: true},
		{ID: "c", Title: "Active two"},
		{ID: "d", Title: "Rejected one", Terminal: true},
	}
	list := AttentionList(items)
	if len(list) != 2 || list[0].ID != "a" || list[1].ID != "c" {
		t.Fatalf("attention list = %+v", list)
	}
	if len(AttentionList(nil)) != 0 {
		t.Fatal("nil stream lists attention")
	}
	if len(AttentionList([]WorkItem{{ID: "x", Terminal: true}})) != 0 {
		t.Fatal("terminal-only stream lists attention")
	}

	// The list never aliases its input.
	list[0].Title = "mutated"
	again := AttentionList(items)
	if again[0].Title != "Active one" || items[0].Title != "Active one" {
		t.Fatal("attention list aliases its input")
	}
}

// Golden: attention outcomes over admitted streams.
func TestTodo_WEB_098_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a"}, {ID: "b", Terminal: true}, {ID: "c"}},
		{{ID: "x", Terminal: true}, {ID: "y", Terminal: true}},
		{{ID: "solo"}},
		{{ID: "dup"}, {ID: "dup"}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		for _, item := range AttentionList(stream) {
			builder.WriteString(item.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "0d78cda730acf72be84703dcb7c182d3cd96ddaedfe1648c7eb9352445518c60"
	if got != want {
		t.Fatalf("attention digest = %s, want %s", got, want)
	}
}

// Browser: attention over admitted-stream patterns preserves
// admission order deterministically.
func TestTodo_WEB_098_Browser(t *testing.T) {
	patterns := [][]WorkItem{
		{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		{{ID: "c", Terminal: true}, {ID: "b"}, {ID: "a", Terminal: true}},
		{{ID: "z"}, {ID: "y", Terminal: true}, {ID: "x"}, {ID: "w", Terminal: true}, {ID: "v"}},
	}
	for _, stream := range patterns {
		first := AttentionList(stream)
		second := AttentionList(stream)
		previous := -1
		for _, item := range first {
			current := -1
			for i, candidate := range stream {
				if candidate.ID == item.ID && !candidate.Terminal {
					current = i
					break
				}
			}
			if current < 0 || current < previous {
				t.Fatalf("admission order broken: %+v from %+v", first, stream)
			}
			previous = current
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("attention list is nondeterministic")
		}
	}
}

// Conformance: terminal flags decide alone, fields pass
// through untouched, stability.
func TestTodo_WEB_098_Conformance(t *testing.T) {
	full := WorkItem{ID: "a", Title: "T", Person: "P", Status: "S", Due: "D", Tone: "T", Href: "H", Terminal: false}
	list := AttentionList([]WorkItem{full})
	if len(list) != 1 || !reflect.DeepEqual(list[0], full) {
		t.Fatalf("attention rewrites items: %+v", list)
	}
	terminal := full
	terminal.Terminal = true
	if len(AttentionList([]WorkItem{terminal})) != 0 {
		t.Fatal("flag alone does not decide")
	}
	first := AttentionList([]WorkItem{full})
	second := AttentionList([]WorkItem{full})
	if !reflect.DeepEqual(first, second) {
		t.Fatal("attention list is unstable")
	}
}
