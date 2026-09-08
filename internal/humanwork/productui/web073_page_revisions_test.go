package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-073: immutable PageDefinition revisions. The registry
// owns live definitions, but no immutable revision exists anywhere in
// the lifecycle draft → validate → preview → publish chain: a
// published page cannot pin consumers, roll back, or prove what was
// published. Presentation needs an append-only, digest-addressed
// revision log over render-free snapshots — recorded once, never
// mutated — with canonical persisted bytes the studio platform can
// store and re-verify.
func TestTodo_WEB_073(t *testing.T) {
	snap := SnapshotPageDefinition(PageDefinition{ID: "home", Route: "/workspace/app/home", Label: "Home",
		SearchTerms: []string{"start", "landing"}, PrimaryNav: true, RenderOrder: 10})
	if snap.Page != PageHome || snap.Route != "/workspace/app/home" || snap.Label != "Home" || !snap.PrimaryNav || snap.RenderOrder != 10 {
		t.Fatalf("snapshot loses definition fields: %+v", snap)
	}
	if !reflect.DeepEqual(snap.SearchTerms, []string{"start", "landing"}) {
		t.Fatalf("snapshot loses search terms: %+v", snap)
	}

	var log PageRevisionLog
	first, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || first.Digest == "" {
		t.Fatalf("recorded revision misses version/digest: %+v", first)
	}
	// Recording the same snapshot at the same version is idempotent.
	again, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, again) {
		t.Fatal("idempotent re-record changes the revision")
	}
	// The same version with different content is a conflict: published
	// history is immutable.
	changed := snap
	changed.SearchTerms = []string{"start"}
	if _, err := log.Record(snap.Page, changed, 1); err == nil {
		t.Fatal("conflicting re-record at version 1 is accepted")
	}
	// A new version records alongside the old one.
	second, err := log.Record(snap.Page, changed, 2)
	if err != nil {
		t.Fatal(err)
	}
	if latest, ok := log.Latest(snap.Page); !ok || latest.Version != 2 || latest.Digest != second.Digest {
		t.Fatalf("latest revision = %+v, want version 2", latest)
	}
	if _, ok := log.Revision(snap.Page, 9); ok {
		t.Fatal("missing revision resolves")
	}
	if _, err := log.Record(snap.Page, snap, 0); err == nil {
		t.Fatal("zero version is accepted")
	}

	// The log deep-copies: mutating the source or the retrieved
	// snapshot never mutates the record.
	snap.SearchTerms[0] = "MUTATED"
	stored, ok := log.Revision(snap.Page, 1)
	if !ok {
		t.Fatal("recorded revision is missing")
	}
	if !reflect.DeepEqual(stored.Snapshot.SearchTerms, []string{"start", "landing"}) {
		t.Fatalf("stored snapshot mutated through the source: %+v", stored.Snapshot.SearchTerms)
	}
	stored.Snapshot.SearchTerms[0] = "MUTATED"
	fresh, _ := log.Revision(snap.Page, 1)
	if !reflect.DeepEqual(fresh.Snapshot.SearchTerms, []string{"start", "landing"}) {
		t.Fatal("stored snapshot mutates through retrieval")
	}

	// Persisted bytes round-trip and tampering fails closed.
	encoded := MarshalRevision(first)
	parsed, err := ParseRevision(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, first) {
		t.Fatal("persisted revision does not round-trip")
	}
	tampered := append([]byte(nil), encoded...)
	if index := strings.Index(string(tampered), "landing"); index < 0 {
		t.Fatal("persisted bytes miss the search terms")
	} else {
		tampered[index] = 'X'
	}
	if _, err := ParseRevision(tampered); err == nil {
		t.Fatal("tampered persisted bytes are accepted")
	}
	if _, err := ParseRevision([]byte("not a revision")); err == nil {
		t.Fatal("garbage persisted bytes are accepted")
	}
}

// Golden: canonical revision digests for fixed snapshots.
func TestTodo_WEB_073_Golden(t *testing.T) {
	snapshots := []PageDefinitionSnapshot{
		{Page: "home", Route: "/workspace/app/home", Label: "Home", SearchTerms: []string{"start"}, RenderOrder: 10},
		{Page: "people", Route: "/workspace/app/people", Label: "People", SearchTerms: []string{"directory", "workers"}, PrimaryNav: true, RenderOrder: 20},
	}
	var builder strings.Builder
	for _, snap := range snapshots {
		for _, version := range []int64{1, 2} {
			builder.WriteString(string(snap.Page))
			builder.WriteString("\x00")
			builder.WriteString(DigestRevision(snap, version))
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "db66afe2c67b84d3c54550f91ff99bcf5477612021c2b9774b3df377765ae7a4"
	if got != want {
		t.Fatalf("revision matrix digest = %s, want %s", got, want)
	}
}

// Browser: every registered definition records, round-trips through
// persisted text bytes, and verifies — deterministically.
func TestTodo_WEB_073_Browser(t *testing.T) {
	definitions := PageDefinitions()
	var log PageRevisionLog
	for _, definition := range definitions {
		if _, err := log.Record(definition.ID, SnapshotPageDefinition(definition), 1); err != nil {
			t.Fatalf("registry page %q does not record: %v", definition.ID, err)
		}
	}
	if pages := log.Pages(); len(pages) != len(definitions) {
		t.Fatalf("revision log covers %d pages, want %d", len(pages), len(definitions))
	}
	for _, definition := range definitions {
		recorded, ok := log.Revision(definition.ID, 1)
		if !ok {
			t.Fatalf("registry page %q has no revision", definition.ID)
		}
		again, err := log.Record(definition.ID, SnapshotPageDefinition(definition), 1)
		if err != nil {
			t.Fatal(err)
		}
		if again.Digest != recorded.Digest {
			t.Fatalf("registry page %q digest is unstable", definition.ID)
		}
		// Persisted textbytes survive a storage round-trip byte-for-byte.
		rehydrated, err := ParseRevision([]byte(string(MarshalRevision(recorded))))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rehydrated, recorded) {
			t.Fatalf("registry page %q does not survive persisted bytes", definition.ID)
		}
	}
}

// Conformance: cross-instance determinism, out-of-order versions,
// empty-log behavior.
func TestTodo_WEB_073_Conformance(t *testing.T) {
	snap := PageDefinitionSnapshot{Page: "home", Route: "/workspace/app/home", Label: "Home", SearchTerms: []string{"start"}}
	var first, second PageRevisionLog
	for _, log := range []*PageRevisionLog{&first, &second} {
		if _, err := log.Record(snap.Page, snap, 2); err != nil {
			t.Fatal(err)
		}
		if _, err := log.Record(snap.Page, snap, 1); err != nil {
			t.Fatal(err)
		}
	}
	latest, ok := first.Latest(snap.Page)
	if !ok || latest.Version != 2 {
		t.Fatal("latest does not track the maximum version")
	}
	other, _ := second.Latest(snap.Page)
	if !reflect.DeepEqual(latest, other) {
		t.Fatal("revision logs disagree across instances")
	}
	var empty PageRevisionLog
	if _, ok := empty.Latest("home"); ok {
		t.Fatal("empty log resolves a latest revision")
	}
	if pages := empty.Pages(); len(pages) != 0 {
		t.Fatal("empty log inventories pages")
	}
}
