package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-107: completed-work history. Recent work
// (WEB-099) feeds the slot with full items, but nothing
// projects the history record: pages hand-pick completion
// evidence per row, so the history disagrees with the
// recent-work list about what was completed by convention.
// The compiler needs the derived-only record — one entry
// per terminal item in admission order carrying exactly the
// completion evidence — adding no truth.
func TestTodo_WEB_107(t *testing.T) {
	items := []WorkItem{
		{ID: "a", Title: "Active one"},
		{ID: "b", Title: "Done one", CompletedAt: "2026-09-01", Terminal: true},
		{ID: "c", Title: "Rejected one", CompletedAt: "2026-09-02", Terminal: true},
	}
	history := CompletedHistory(items)
	if len(history) != 2 || history[0].ID != "b" || history[0].CompletedAt != "2026-09-01" {
		t.Fatalf("history = %+v", history)
	}
	if len(CompletedHistory(nil)) != 0 {
		t.Fatal("nil stream has history")
	}
	if len(CompletedHistory([]WorkItem{{ID: "x"}})) != 0 {
		t.Fatal("active-only stream has history")
	}

	// History covers exactly the recent-work list.
	var recentIDs []string
	for _, item := range RecentWork(items) {
		recentIDs = append(recentIDs, item.ID)
	}
	var historyIDs []string
	for _, entry := range history {
		historyIDs = append(historyIDs, entry.ID)
	}
	if !reflect.DeepEqual(historyIDs, recentIDs) {
		t.Fatalf("history %q vs recent work %q", historyIDs, recentIDs)
	}
}

// Golden: history outcomes over admitted streams.
func TestTodo_WEB_107_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a"}},
		{{ID: "a", CompletedAt: "2026-09-01", Terminal: true}},
		{{ID: "x", CompletedAt: "2026-09-01", Terminal: true}, {ID: "y", Terminal: true}, {ID: "z"}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		for _, entry := range CompletedHistory(stream) {
			fmt.Fprintf(&builder, "%s|%s\x00", entry.ID, entry.CompletedAt)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "b7e87ea14f4c680d62ade09abd74eca83737e9aa2d43533fe94d6feeb7f00176"
	if got != want {
		t.Fatalf("history digest = %s, want %s", got, want)
	}
}

// Browser: history over stream patterns keeps admission
// order deterministically.
func TestTodo_WEB_107_Browser(t *testing.T) {
	patterns := [][]WorkItem{
		{{ID: "a", CompletedAt: "2026-09-01", Terminal: true}, {ID: "b"}, {ID: "c", CompletedAt: "2026-09-02", Terminal: true}},
		{{ID: "z", Terminal: true}, {ID: "y"}, {ID: "x", CompletedAt: "2026-09-03", Terminal: true}},
	}
	for _, stream := range patterns {
		first := CompletedHistory(stream)
		second := CompletedHistory(stream)
		previous := -1
		for _, entry := range first {
			current := -1
			for i, candidate := range stream {
				if candidate.ID == entry.ID {
					if !candidate.Terminal {
						t.Fatalf("active item in history: %+v", entry)
					}
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
			t.Fatal("history is nondeterministic")
		}
	}
}

// Conformance: one entry per terminal item, evidence passes
// through, stability.
func TestTodo_WEB_107_Conformance(t *testing.T) {
	stream := []WorkItem{
		{ID: "a"},
		{ID: "b", CompletedAt: "2026-09-01", Terminal: true},
	}
	history := CompletedHistory(stream)
	if len(history) != 1 || history[0].ID != "b" || history[0].CompletedAt != "2026-09-01" {
		t.Fatalf("history rewrites items: %+v", history)
	}
	if len(history) != len(RecentWork(stream)) {
		t.Fatal("history disagrees with recent work")
	}
	if !reflect.DeepEqual(history, CompletedHistory(stream)) {
		t.Fatal("history is unstable")
	}
}
