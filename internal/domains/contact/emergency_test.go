package contact_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/contact"
)

func emergencySet(t *testing.T) contact.EmergencyContactSet {
	t.Helper()
	set := contact.NewSet("jane")
	var err error
	set, err = contact.Add(set, contact.EmergencyContact{
		PersonName: "Kim", Relationship: contact.RelationshipSpouse, Priority: 1,
		Reachability: []string{"+1-555-0101"}, Notes: "uses video relay",
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err = contact.Add(set, contact.EmergencyContact{
		PersonName: "Jo", Relationship: contact.RelationshipParent, Priority: 2,
		Reachability: []string{"jo@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// TestEmergencyContactConformancePreservesRelationshipMeaningAndOrderedSet
// is the CONF-018 primary test: ADD/REVISE/REMOVE/REORDER/CORRECT keep
// exact membership, unique integer priorities, reachability, governed
// person linkage and minimum disclosure, while relationship labels grant
// no authority and external observations reconcile or open scoped repair.
func TestEmergencyContactConformancePreservesRelationshipMeaningAndOrderedSet(t *testing.T) {
	set := emergencySet(t)
	ordered := contact.Ordered(set)
	if len(ordered) != 2 || ordered[0].Priority != 1 || ordered[1].Priority != 2 {
		t.Fatalf("ordered = %+v, want priorities 1 then 2", ordered)
	}
	if ordered[0].ID == "" || ordered[0].ID == ordered[1].ID {
		t.Fatalf("add assigned no distinct IDs: %+v", ordered)
	}

	// Relationship meaning without authority: labels read, never grant.
	if ordered[0].Relationship != contact.RelationshipSpouse {
		t.Fatalf("relationship = %s, want SPOUSE", ordered[0].Relationship)
	}
	if authority := ordered[0].AuthorityGranted(); len(authority) != 0 {
		t.Fatalf("label grants authority: %v", authority)
	}
	for _, kind := range []string{"dependent", "beneficiary", "representative"} {
		if err := ordered[0].AuthorityOf(kind); !errors.Is(err, contact.ErrLabelAuthority) {
			t.Fatalf("label authority %s = %v, want ErrLabelAuthority", kind, err)
		}
	}

	// The supplied person is display-only until governed linkage.
	if ordered[0].IsCanonical() {
		t.Fatal("unlinked person reads canonical")
	}
	if _, err := contact.CanonicalPerson(set, ordered[0].ID); !errors.Is(err, contact.ErrUnlinkedPerson) {
		t.Fatalf("unlinked canonical = %v, want ErrUnlinkedPerson", err)
	}
	set, err := contact.LinkPerson(set, ordered[0].ID, "person/kim-1", "evidence/hr-file-9")
	if err != nil {
		t.Fatal(err)
	}
	person, err := contact.CanonicalPerson(set, ordered[0].ID)
	if err != nil || person != "person/kim-1" {
		t.Fatalf("canonical = %q, %v; want person/kim-1", person, err)
	}

	// Minimum disclosure: standard views hide reachability and notes;
	// emergency views show them; nothing else changes.
	standard := contact.Disclose(set, false)
	if len(standard) != 2 || standard[0].Reachability != nil || standard[0].Notes != "" {
		t.Fatalf("standard disclosure leaks: %+v", standard)
	}
	emergency := contact.Disclose(set, true)
	if len(emergency[0].Reachability) != 1 || emergency[0].Notes != "uses video relay" {
		t.Fatalf("emergency disclosure hides: %+v", emergency)
	}

	// REORDER restates the full order; REVISE, CORRECT and REMOVE keep
	// exact membership under new revisions.
	ids := map[string]int{ordered[0].ID: 2, ordered[1].ID: 1}
	set, err = contact.Reorder(set, ids)
	if err != nil {
		t.Fatal(err)
	}
	if contact.Ordered(set)[0].ID != ordered[1].ID {
		t.Fatal("reorder did not restate the order")
	}
	revised := contact.Ordered(set)[0]
	revised.Reachability = []string{"+1-555-0102"}
	set, err = contact.Revise(set, revised)
	if err != nil {
		t.Fatal(err)
	}
	corrected := contact.Ordered(set)[1]
	corrected.Notes = "prefers text"
	set, err = contact.Correct(set, corrected, "note correction")
	if err != nil {
		t.Fatal(err)
	}
	set, err = contact.Remove(set, corrected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Members) != 1 {
		t.Fatalf("members = %d, want exactly one after remove", len(set.Members))
	}

	// Exact external observation reconciles; provider acceptance alone
	// never closes.
	remaining := contact.Ordered(set)
	status, err := contact.Reconcile(set, contact.Observation{Members: remaining, Accepted: true})
	if err != nil || status != "reconciled" {
		t.Fatalf("reconcile = %q, %v; want reconciled", status, err)
	}
	status, err = contact.Reconcile(set, contact.Observation{Members: remaining})
	if err != nil || status != "reconciled" {
		t.Fatalf("unaccepted exact observation = %q, %v; acceptance is evidence, not closure", status, err)
	}
	drifted := append([]contact.EmergencyContact(nil), remaining...)
	drifted[0].Priority = 9
	status, err = contact.Reconcile(set, contact.Observation{Members: drifted, Accepted: true})
	if err != nil || status != "repair/order" {
		t.Fatalf("drifted reconcile = %q, %v; want scoped repair/order", status, err)
	}
}

func TestTodo_CONF_018_Property(t *testing.T) {
	// Ordered-set invariants hold across operation sequences: unique
	// positive priorities, stable membership counts and digest changes on
	// every accepted mutation.
	set := emergencySet(t)
	before := set.Digest
	seen := map[int]bool{}
	for _, member := range contact.Ordered(set) {
		if member.Priority < 1 || seen[member.Priority] {
			t.Fatalf("priority invariant broken: %+v", member)
		}
		seen[member.Priority] = true
	}
	ids := map[string]int{}
	for _, member := range set.Members {
		ids[member.ID] = 3 - member.Priority
	}
	set, err := contact.Reorder(set, ids)
	if err != nil {
		t.Fatal(err)
	}
	if set.Digest == before || len(set.Members) != 2 {
		t.Fatal("reorder changed nothing or lost members")
	}
	// Priorities survive remove as explicit gaps: removing priority 1
	// leaves priority 2 standing, never renumbered to 1.
	removed, err := contact.Remove(set, contact.Ordered(set)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Members) != 1 || removed.Members[0].Priority != 2 {
		t.Fatalf("remove renumbered priorities: %+v", removed.Members)
	}
}

func TestTodo_CONF_018_Golden(t *testing.T) {
	set := emergencySet(t)
	var err error
	set, err = contact.LinkPerson(set, contact.Ordered(set)[0].ID, "person/kim-1", "evidence/hr-file-9")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "worker: jane revision: %d digest: %s\n", set.Revision, set.Digest)
	for _, member := range contact.Ordered(set) {
		fmt.Fprintf(&b, "member: %s %s priority=%d linked=%v\n", member.ID, member.Relationship, member.Priority, member.PersonLinked)
	}
	for _, view := range contact.Disclose(set, false) {
		fmt.Fprintf(&b, "disclosed: %s %s priority=%d\n", view.ID, view.Relationship, view.Priority)
	}
	status, err := contact.Reconcile(set, contact.Observation{Members: contact.Ordered(set), Accepted: true})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "reconcile: %s\n", status)
	got := b.String()
	path := filepath.Join("testdata", "conf018_emergency.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestTodo_CONF_018_Race(t *testing.T) {
	directory := contact.NewDirectory()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := directory.Apply("jane", func(set contact.EmergencyContactSet) (contact.EmergencyContactSet, error) {
				return contact.Add(set, contact.EmergencyContact{
					PersonName: fmt.Sprintf("kin-%d", i), Relationship: contact.RelationshipFriend,
					Priority: i + 1, Reachability: []string{fmt.Sprintf("+1-555-%04d", i)},
				})
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent add = %v", err)
	}
	set, ok := directory.Get("jane")
	if !ok || len(set.Members) != workers {
		t.Fatalf("directory holds %d members, want %d", len(set.Members), workers)
	}
	seen := map[int]bool{}
	for _, member := range set.Members {
		if seen[member.Priority] {
			t.Fatalf("priority %d collided under concurrency", member.Priority)
		}
		seen[member.Priority] = true
	}
}

// TestTodo_CONF_018_Integration reconciles a drifted provider
// observation through scoped repair and redrives only the mismatch.
func TestTodo_CONF_018_Integration(t *testing.T) {
	set := emergencySet(t)
	members := contact.Ordered(set)
	// The provider reports Jo unreachable at a stale address: values
	// drift opens scoped repair, never a full rewrite.
	drifted := append([]contact.EmergencyContact(nil), members...)
	drifted[1].Reachability = []string{"stale@example.com"}
	status, err := contact.Reconcile(set, contact.Observation{Members: drifted, Accepted: true})
	if err != nil || status != "repair/values" {
		t.Fatalf("reconcile = %q, %v; want scoped repair/values", status, err)
	}
	// Repair redrives only the drifted member: Jo confirms the address
	// out of band, the local revision carries it, and the provider's
	// next observation — caught up — reconciles exactly.
	confirmed := drifted[1]
	confirmed.Reachability = []string{"jo@example.com"}
	repaired, err := contact.Revise(set, confirmed)
	if err != nil {
		t.Fatal(err)
	}
	if len(repaired.Members) != len(set.Members) {
		t.Fatal("repair rewrote membership instead of redriving values")
	}
	caughtUp := append([]contact.EmergencyContact(nil), members...)
	caughtUp[1].Reachability = []string{"jo@example.com"}
	status, err = contact.Reconcile(repaired, contact.Observation{Members: caughtUp, Accepted: true})
	if err != nil || status != "reconciled" {
		t.Fatalf("post-repair reconcile = %q, %v", status, err)
	}
}

func TestTodo_CONF_018_Fault(t *testing.T) {
	set := emergencySet(t)
	// Missing IDs conflate nothing: add with ID and revise without one.
	withID := contact.EmergencyContact{ID: "ec/mine", Relationship: contact.RelationshipFriend, Priority: 3}
	if _, err := contact.Add(set, withID); !errors.Is(err, contact.ErrAddWithID) {
		t.Fatalf("add-with-id = %v, want ErrAddWithID", err)
	}
	withoutID := contact.EmergencyContact{Relationship: contact.RelationshipFriend, Priority: 3}
	if _, err := contact.Revise(set, withoutID); !errors.Is(err, contact.ErrMissingID) {
		t.Fatalf("revise-without-id = %v, want ErrMissingID", err)
	}
	// Partial reorder loses members and is refused.
	partial := map[string]int{contact.Ordered(set)[0].ID: 1}
	if _, err := contact.Reorder(set, partial); !errors.Is(err, contact.ErrMemberLost) {
		t.Fatalf("partial reorder = %v, want ErrMemberLost", err)
	}
	// Colliding priorities are refused.
	colliding := map[string]int{}
	for _, member := range set.Members {
		colliding[member.ID] = 1
	}
	if _, err := contact.Reorder(set, colliding); !errors.Is(err, contact.ErrPriorityCollision) {
		t.Fatalf("priority collision = %v, want ErrPriorityCollision", err)
	}
	// Unknown members and reasonless corrections are refused.
	ghost := contact.EmergencyContact{ID: "ec/ghost", Relationship: contact.RelationshipFriend, Priority: 3}
	if _, err := contact.Revise(set, ghost); !errors.Is(err, contact.ErrUnknownMember) {
		t.Fatalf("ghost revise = %v, want ErrUnknownMember", err)
	}
	if _, err := contact.Correct(set, contact.Ordered(set)[0], ""); err == nil {
		t.Fatal("reasonless correction accepted")
	}
	// Unknown relationships are refused everywhere.
	alien := contact.EmergencyContact{PersonName: "X", Relationship: "MANAGER", Priority: 3}
	if _, err := contact.Add(set, alien); err == nil {
		t.Fatal("unknown relationship added")
	}
}

func TestTodo_CONF_018_Security(t *testing.T) {
	set := emergencySet(t)
	// Inaccessible notes never leak through any disclosure path.
	for _, view := range contact.Disclose(set, false) {
		if view.Notes != "" || len(view.Reachability) != 0 {
			t.Fatalf("standard disclosure leaks: %+v", view)
		}
	}
	// Emergency disclosure is still minimum: Kim's notes show while
	// Jo's empty notes stay empty — nothing is invented.
	views := contact.Disclose(set, true)
	if !strings.Contains(views[0].Notes, "video relay") {
		t.Fatalf("emergency disclosure hides notes: %+v", views[0])
	}
	if views[1].Notes != "" {
		t.Fatalf("emergency disclosure invents notes: %+v", views[1])
	}
	// Labels grant nothing under any spelling.
	member := contact.Ordered(set)[0]
	for _, kind := range []string{"Dependent", "BENEFICIARY", "Representative"} {
		if err := member.AuthorityOf(kind); !errors.Is(err, contact.ErrLabelAuthority) {
			t.Fatalf("label authority %s = %v, want ErrLabelAuthority", kind, err)
		}
	}
	// Unlinked persons never resolve canonical, even with a name.
	if _, err := contact.CanonicalPerson(set, member.ID); !errors.Is(err, contact.ErrUnlinkedPerson) {
		t.Fatalf("unlinked canonical = %v, want ErrUnlinkedPerson", err)
	}
}

func TestTodo_CONF_018_Conformance(t *testing.T) {
	// The operation vocabulary is closed: add assigns, revise/reorder/
	// correct/remove advance the revision, and every step reseals.
	set := emergencySet(t)
	rev := set.Revision
	steps := []func(contact.EmergencyContactSet) (contact.EmergencyContactSet, error){
		func(s contact.EmergencyContactSet) (contact.EmergencyContactSet, error) {
			return contact.Add(s, contact.EmergencyContact{PersonName: "Al", Relationship: contact.RelationshipSibling, Priority: 3})
		},
		func(s contact.EmergencyContactSet) (contact.EmergencyContactSet, error) {
			next := contact.Ordered(s)[0]
			next.Notes = "rev"
			return contact.Revise(s, next)
		},
		func(s contact.EmergencyContactSet) (contact.EmergencyContactSet, error) {
			ids := map[string]int{}
			for i, member := range contact.Ordered(s) {
				ids[member.ID] = i + 1
			}
			return contact.Reorder(s, ids)
		},
		func(s contact.EmergencyContactSet) (contact.EmergencyContactSet, error) {
			return contact.Correct(s, contact.Ordered(s)[0], "spelling")
		},
		func(s contact.EmergencyContactSet) (contact.EmergencyContactSet, error) {
			return contact.Remove(s, contact.Ordered(s)[0].ID)
		},
	}
	for i, step := range steps {
		next, err := step(set)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if next.Revision != rev+1 || next.Digest == "" || next.Digest == set.Digest {
			t.Fatalf("step %d broke revision sealing: %+v", i, next)
		}
		set = next
		rev = next.Revision
	}
	if len(set.Members) != 2 {
		t.Fatalf("members = %d, want the add/remove pair to net zero", len(set.Members))
	}
}

func TestTodo_CONF_018_Mutation(t *testing.T) {
	set := emergencySet(t)
	// Mutant 1: a duplicate priority injected by revise is refused.
	dup := contact.Ordered(set)[0]
	dup.Priority = contact.Ordered(set)[1].Priority
	if _, err := contact.Revise(set, dup); !errors.Is(err, contact.ErrPriorityCollision) {
		t.Fatalf("duplicate priority = %v, want ErrPriorityCollision", err)
	}
	// Mutant 2: non-positive priorities are refused.
	zero := contact.Ordered(set)[0]
	zero.Priority = 0
	if _, err := contact.Revise(set, zero); !errors.Is(err, contact.ErrPriorityCollision) {
		t.Fatalf("zero priority = %v, want ErrPriorityCollision", err)
	}
	// Mutant 3: linking without evidence is refused.
	if _, err := contact.LinkPerson(set, contact.Ordered(set)[0].ID, "person/x", ""); err == nil {
		t.Fatal("evidenceless linkage accepted")
	}
	// Mutant 4: removing the unknown member is refused.
	if _, err := contact.Remove(set, "ec/ghost"); !errors.Is(err, contact.ErrUnknownMember) {
		t.Fatalf("ghost remove = %v, want ErrUnknownMember", err)
	}
	// Mutant 5: membership drift opens repair, never silent reconcile.
	short := contact.Ordered(set)[:1]
	status, err := contact.Reconcile(set, contact.Observation{Members: short, Accepted: true})
	if err != nil || status != "repair/membership" {
		t.Fatalf("short observation = %q, %v; want repair/membership", status, err)
	}
}
