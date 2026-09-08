package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-105: the Draft Center view. UF-002 gives every
// initiator a save-and-resume surface, but nothing composes
// the resumable set: pages hand-combine viewer scoping with
// unfinished/unsubmitted checks, so review-awaiting or
// unowned work lands in the draft list by convention. The
// compiler needs the governed view — viewer-owned, open, and
// not awaiting approval — composed from the existing
// collection and filter truth without adding any.
func TestTodo_WEB_105(t *testing.T) {
	viewer := ViewerProfile{PersonID: "p-amy"}
	items := []WorkItem{
		{ID: "a", PersonRef: "p-amy", Status: "In progress"},
		{ID: "b", PersonRef: "p-amy", Status: "Awaiting approval"},
		{ID: "c", PersonRef: "p-amy", Status: "Done", Terminal: true},
		{ID: "d", PersonRef: "p-bob", Status: "In progress"},
		{ID: "e", PersonRef: "p-amy", Status: "Blocked"},
	}
	drafts := DraftCenterItems(items, viewer)
	var ids []string
	for _, item := range drafts {
		ids = append(ids, item.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a", "e"}) {
		t.Fatalf("drafts = %+v", drafts)
	}
	if len(DraftCenterItems(items, ViewerProfile{})) != 0 {
		t.Fatal("empty viewer resumes drafts")
	}
	if len(DraftCenterItems(nil, viewer)) != 0 {
		t.Fatal("nil stream holds drafts")
	}
}

// Golden: draft-center outcomes over viewer/stream pairs.
func TestTodo_WEB_105_Golden(t *testing.T) {
	streams := [][]WorkItem{
		nil,
		{},
		{{ID: "a", PersonRef: "p-amy", Status: "In progress"}},
		{{ID: "a", PersonRef: "p-amy", Status: "Awaiting approval"}, {ID: "b", PersonRef: "p-amy", Status: "Blocked"}},
		{{ID: "x", PersonRef: "p-bob", Status: "In progress"}, {ID: "y", PersonRef: "p-amy", Status: "Done", Terminal: true}},
	}
	viewers := []ViewerProfile{{PersonID: "p-amy"}, {}, {PersonID: "p-ghost"}}
	var builder strings.Builder
	for _, viewer := range viewers {
		for _, stream := range streams {
			for _, item := range DraftCenterItems(stream, viewer) {
				builder.WriteString(item.ID)
				builder.WriteString("\x00")
			}
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "bc816d682a551d87e097c86b8401d0c8cd5124c884297bc9cdf715227e861794"
	if got != want {
		t.Fatalf("draft-center digest = %s, want %s", got, want)
	}
}

// Browser: draft outcomes over patterns keep admission
// order deterministically.
func TestTodo_WEB_105_Browser(t *testing.T) {
	viewer := ViewerProfile{PersonID: "p-amy"}
	patterns := [][]WorkItem{
		{{ID: "a", PersonRef: "p-amy", Status: "In progress"}, {ID: "b", PersonRef: "p-amy", Status: "Awaiting approval"}},
		{{ID: "c", PersonRef: "p-bob", Status: "In progress"}, {ID: "d", PersonRef: "p-amy", Status: "Blocked"}},
		{{ID: "e", PersonRef: "p-amy", Status: "Done", Terminal: true}, {ID: "f", PersonRef: "p-amy", Status: "In progress"}},
	}
	for _, stream := range patterns {
		first := DraftCenterItems(stream, viewer)
		second := DraftCenterItems(stream, viewer)
		previous := -1
		for _, item := range first {
			if item.PersonRef != "p-amy" || item.Terminal || item.Status == "Awaiting approval" {
				t.Fatalf("non-resumable draft: %+v", item)
			}
			current := -1
			for i, candidate := range stream {
				if candidate.ID == item.ID {
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
			t.Fatal("draft center is nondeterministic")
		}
	}
}

// Conformance: drafts stay inside My Work, outside review
// and terminal; passthrough; stability.
func TestTodo_WEB_105_Conformance(t *testing.T) {
	viewer := ViewerProfile{PersonID: "p-amy"}
	stream := []WorkItem{
		{ID: "a", PersonRef: "p-amy", Status: "In progress", Title: "T"},
		{ID: "b", PersonRef: "p-amy", Status: "Awaiting approval"},
		{ID: "c", PersonRef: "p-bob", Status: "In progress"},
	}
	drafts := DraftCenterItems(stream, viewer)
	if len(drafts) != 1 || !reflect.DeepEqual(drafts[0], stream[0]) {
		t.Fatalf("draft center rewrites items: %+v", drafts)
	}
	mine := MyWorkItems(stream, viewer)
	inMine := false
	for _, item := range mine {
		if item.ID == drafts[0].ID {
			inMine = true
		}
	}
	if !inMine {
		t.Fatal("draft escapes My Work")
	}
	for _, item := range FilterWorkCollection(stream, WorkCollectionReview) {
		for _, draft := range drafts {
			if draft.ID == item.ID {
				t.Fatal("review-awaiting item resumable as draft")
			}
		}
	}
	drafts[0].Title = "mutated"
	if DraftCenterItems(stream, viewer)[0].Title != "T" || stream[0].Title != "T" {
		t.Fatal("draft center aliases its input")
	}
}
