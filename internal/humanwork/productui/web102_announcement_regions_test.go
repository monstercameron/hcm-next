package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-102: governed announcement regions. The shell
// already states the policy — one assertive announcement per
// surface, everything else polite — but nothing enforces it
// for the announcements section: callers hand-split streams,
// so two assertive claims either double-announce to assistive
// technology or get dropped by ad hoc convention. The
// compiler needs the governed split — first assertive claim
// wins in admission order, everything else polite — with
// region membership as the governance and items passing
// through untouched.
func TestTodo_WEB_102(t *testing.T) {
	items := []Announcement{
		{ID: "route", Message: "Page loaded"},
		{ID: "saved", Message: "Draft saved"},
		{ID: "expiry", Message: "Session expiring", Assertive: true},
		{ID: "conflict", Message: "Conflict found", Assertive: true},
	}
	polite, assertive := GovernAnnouncements(items)
	if len(assertive) != 1 || assertive[0].ID != "expiry" {
		t.Fatalf("assertive = %+v", assertive)
	}
	var politeIDs []string
	for _, item := range polite {
		politeIDs = append(politeIDs, item.ID)
	}
	if !reflect.DeepEqual(politeIDs, []string{"route", "saved", "conflict"}) {
		t.Fatalf("polite = %+v", polite)
	}
	polite, assertive = GovernAnnouncements(nil)
	if len(polite) != 0 || len(assertive) != 0 {
		t.Fatal("nil stream announces")
	}
	polite, assertive = GovernAnnouncements([]Announcement{{ID: "a"}, {ID: "b"}})
	if len(polite) != 2 || len(assertive) != 0 {
		t.Fatalf("all-polite split = %+v / %+v", polite, assertive)
	}
}

// Golden: region outcomes over announcement streams.
func TestTodo_WEB_102_Golden(t *testing.T) {
	streams := [][]Announcement{
		nil,
		{},
		{{ID: "a"}},
		{{ID: "a", Assertive: true}, {ID: "b"}},
		{{ID: "a", Assertive: true}, {ID: "b", Assertive: true}, {ID: "c"}},
		{{ID: "x"}, {ID: "y", Assertive: true}},
	}
	var builder strings.Builder
	for _, stream := range streams {
		polite, assertive := GovernAnnouncements(stream)
		for _, item := range polite {
			builder.WriteString("p:" + item.ID + "\x00")
		}
		for _, item := range assertive {
			builder.WriteString("a:" + item.ID + "\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "9b1b07dcfe40b0c9d270603a76551f1e13a0cafd9c6160b279136c8f84ef51d6"
	if got != want {
		t.Fatalf("announcement digest = %s, want %s", got, want)
	}
}

// Browser: splits over stream patterns keep admission order
// deterministically with at most one assertive.
func TestTodo_WEB_102_Browser(t *testing.T) {
	patterns := [][]Announcement{
		{{ID: "a", Assertive: true}, {ID: "b", Assertive: true}},
		{{ID: "b"}, {ID: "a", Assertive: true}, {ID: "c"}},
		{{ID: "z", Assertive: true}, {ID: "y"}, {ID: "x", Assertive: true}, {ID: "w"}},
	}
	for _, stream := range patterns {
		polite, assertive := GovernAnnouncements(stream)
		againPolite, againAssertive := GovernAnnouncements(stream)
		if len(assertive) > 1 {
			t.Fatalf("two assertive regions: %+v", assertive)
		}
		if len(polite)+len(assertive) != len(stream) {
			t.Fatal("split drops or doubles announcements")
		}
		previous := -1
		for _, item := range polite {
			current := -1
			for i, candidate := range stream {
				if candidate.ID == item.ID {
					current = i
					break
				}
			}
			if current < 0 || current < previous {
				t.Fatalf("admission order broken: %+v from %+v", polite, stream)
			}
			previous = current
		}
		if !reflect.DeepEqual(polite, againPolite) || !reflect.DeepEqual(assertive, againAssertive) {
			t.Fatal("split is nondeterministic")
		}
	}
}

// Conformance: total preservation, first-claim wins,
// passthrough identity, stability.
func TestTodo_WEB_102_Conformance(t *testing.T) {
	stream := []Announcement{
		{ID: "a", Message: "A"},
		{ID: "b", Message: "B", Assertive: true},
		{ID: "c", Message: "C", Assertive: true},
	}
	polite, assertive := GovernAnnouncements(stream)
	if len(polite)+len(assertive) != len(stream) {
		t.Fatal("split drops or doubles announcements")
	}
	if len(assertive) != 1 || !reflect.DeepEqual(assertive[0], stream[1]) {
		t.Fatalf("first claim does not win: %+v", assertive)
	}
	if !reflect.DeepEqual(polite[0], stream[0]) || !reflect.DeepEqual(polite[1], stream[2]) {
		t.Fatalf("split rewrites items: %+v", polite)
	}
	againPolite, againAssertive := GovernAnnouncements(stream)
	if !reflect.DeepEqual(polite, againPolite) || !reflect.DeepEqual(assertive, againAssertive) {
		t.Fatal("split is unstable")
	}
}
