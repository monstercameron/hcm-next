package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-112: the worker identity header. Every surface
// hand-assembles the worker header facts — projected name
// here, raw name there, initials and photo by separate
// convention — so a denied name leaks through one header
// while another withholds it. The compiler needs the single
// governed constructor: name and role through the discovery
// projection, initials and photo passing through, reusing
// the exact pattern the search surface already follows.
func TestTodo_WEB_112(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "worker-avery", Name: "Avery Patel", Role: "DES2", Initials: "AP", PhotoURL: "/photos/avery.png"}

	silent := ResolveWorkerIdentity(locale, person, nil)
	if silent.Name != "Avery Patel" || silent.Role != "DES2" || silent.Initials != "AP" || silent.PhotoURL != "/photos/avery.png" {
		t.Fatalf("silent identity = %+v", silent)
	}

	verdicts := map[string]AuthorizedRecord{
		"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow}, "role": {Effect: PresentationDenied, Reason: "Not for this purpose."},
		}},
	}
	governed := ResolveWorkerIdentity(locale, person, verdicts)
	if governed.Name != "Avery Patel" {
		t.Fatalf("allowed name projects as %q", governed.Name)
	}
	if governed.Role != "Unavailable" {
		t.Fatalf("denied role projects as %q", governed.Role)
	}
	if governed.Initials != "AP" || governed.PhotoURL != "/photos/avery.png" {
		t.Fatalf("avatar facts rewritten: %+v", governed)
	}
}

// Golden: identity outcomes over person/verdict pairs.
func TestTodo_WEB_112_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	people := []Person{
		{ID: "worker-avery", Name: "Avery Patel", Role: "DES2", Initials: "AP"},
		{ID: "worker-jordan", Name: "Jordan Lee", Role: "IC3"},
	}
	verdictSets := []map[string]AuthorizedRecord{
		nil,
		{},
		{"worker-avery": {ID: "worker-avery", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow}, "role": {Effect: PresentationDenied},
		}}},
	}
	var builder strings.Builder
	for _, verdicts := range verdictSets {
		for _, person := range people {
			identity := ResolveWorkerIdentity(locale, person, verdicts)
			fmt.Fprintf(&builder, "%s|%s|%s|%s\x00", identity.Name, identity.Role, identity.Initials, identity.PhotoURL)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "299c9677a6716a037bbba4280a1c1511e230371de3828e7d3cff5a3f9262862c"
	if got != want {
		t.Fatalf("identity digest = %s, want %s", got, want)
	}
}

// Browser: identities over person patterns resolve
// deterministically without mutating the record.
func TestTodo_WEB_112_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	patterns := []Person{
		{ID: "w-1", Name: "Ann", Role: "R1", Initials: "A", PhotoURL: "/a.png"},
		{ID: "w-2", Name: "Bob"},
	}
	verdicts := map[string]AuthorizedRecord{
		"w-1": {ID: "w-1", Disclosable: true, Fields: map[string]AuthorizedField{"name": {Effect: PresentationAllow}}},
	}
	for _, person := range patterns {
		before := person
		first := ResolveWorkerIdentity(locale, person, verdicts)
		second := ResolveWorkerIdentity(locale, person, verdicts)
		if !reflect.DeepEqual(person, before) {
			t.Fatal("identity resolution mutates its record")
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("identity resolution is nondeterministic")
		}
		if first.Initials != person.Initials || first.PhotoURL != person.PhotoURL {
			t.Fatalf("avatar facts rewritten: %+v", first)
		}
	}
}

// Conformance: silent servers pass through; governed
// records project name and role exactly like the discovery
// labels the search surface advertises.
func TestTodo_WEB_112_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	person := Person{ID: "w-9", Name: "Nine", Role: "R9", Initials: "N"}
	verdicts := map[string]AuthorizedRecord{
		"w-9": {ID: "w-9", Disclosable: true, Fields: map[string]AuthorizedField{
			"name": {Effect: PresentationAllow}, "role": {Effect: PresentationAllow},
		}},
	}
	identity := ResolveWorkerIdentity(locale, person, verdicts)
	if identity.Name != DiscoveryLabel(locale, person.ID, person.Name, "name", verdicts) ||
		identity.Role != DiscoveryLabel(locale, person.ID, person.Role, "role", verdicts) {
		t.Fatalf("identity diverges from discovery labels: %+v", identity)
	}
	silent := ResolveWorkerIdentity(locale, person, map[string]AuthorizedRecord{})
	if silent.Name != "Nine" || silent.Role != "R9" {
		t.Fatalf("empty verdicts rewrite: %+v", silent)
	}
	again := ResolveWorkerIdentity(locale, person, verdicts)
	if !reflect.DeepEqual(identity, again) {
		t.Fatal("identity resolution is unstable")
	}
}
