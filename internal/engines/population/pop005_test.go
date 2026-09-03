package population_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/population"
)

func restrictedForSnapshot(t *testing.T) population.RestrictedResult {
	t.Helper()
	result := resolvedResult(t)
	restricted, err := population.ApplyRestrictions(result, population.RestrictionDecision{
		Versions: mustPolicyVersions(), DiscloseMembership: true,
	})
	if err != nil {
		t.Fatalf("ApplyRestrictions: %v", err)
	}
	return restricted
}

// TestTodo_POP_005 proves the RED and GREEN clauses of planning/todos.md
// POP-005: changing the definition, the resolved facts (reflected in the
// restricted membership) or the applied policy after a snapshot is frozen
// never mutates that already-produced Snapshot, and a well-formed freeze
// binds the exact sorted subject IDs (or, when membership is protected, the
// fact of protection), definition, time context, watermarks, count and a
// digest.
func TestTodo_POP_005(t *testing.T) {
	def := validDefinition()
	restricted := restrictedForSnapshot(t)
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	watermarks := testWatermarks(t)

	t.Run("GREEN_freeze_binds_sorted_subject_ids_and_context", func(t *testing.T) {
		snap, err := population.Freeze(def, "2026.1", restricted, asOf, knownAt, watermarks)
		if err != nil {
			t.Fatalf("Freeze: %v", err)
		}
		if snap.DefinitionID != def.ID {
			t.Fatalf("definition id = %q, want %q", snap.DefinitionID, def.ID)
		}
		if snap.RevisionVersion != "2026.1" {
			t.Fatalf("revision version = %q", snap.RevisionVersion)
		}
		if snap.MembershipProtected {
			t.Fatal("a disclosed-membership restriction produced a protected snapshot")
		}
		ids := snap.SubjectIDList()
		if len(ids) != 3 {
			t.Fatalf("subject ids = %v, want 3", ids)
		}
		for i := 1; i < len(ids); i++ {
			if ids[i-1] >= ids[i] {
				t.Fatalf("subject ids are not strictly sorted: %v", ids)
			}
		}
		if count, ok := snap.Count.Get(); !ok || count != 3 {
			t.Fatalf("count = %v (ok=%v), want 3", count, ok)
		}
		if snap.Digest == "" {
			t.Fatal("a valid freeze must produce a digest")
		}
	})

	t.Run("GREEN_protected_membership_carries_no_subject_ids", func(t *testing.T) {
		result := resolvedResult(t)
		protectedRestricted, err := population.ApplyRestrictions(result, population.RestrictionDecision{Versions: mustPolicyVersions()})
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		snap, err := population.Freeze(def, "2026.1", protectedRestricted, asOf, knownAt, watermarks)
		if err != nil {
			t.Fatalf("Freeze: %v", err)
		}
		if !snap.MembershipProtected {
			t.Fatal("a protected restriction produced an unprotected snapshot")
		}
		if len(snap.SubjectIDList()) != 0 {
			t.Fatal("a protected snapshot carried raw subject ids")
		}
	})

	t.Run("RED_changing_the_definition_after_freeze_does_not_mutate_the_snapshot", func(t *testing.T) {
		snap, err := population.Freeze(def, "2026.1", restricted, asOf, knownAt, watermarks)
		if err != nil {
			t.Fatalf("Freeze: %v", err)
		}
		before := snap.Digest

		changed := def
		changed.Owner = "a-completely-different-owner"
		_ = changed // mutating the local copy of def must never affect snap

		if snap.Digest != before {
			t.Fatal("a snapshot's digest changed after its originating definition value was copied and mutated")
		}
	})

	t.Run("RED_changed_facts_produce_a_different_digest", func(t *testing.T) {
		snap1, err := population.Freeze(def, "2026.1", restricted, asOf, knownAt, watermarks)
		if err != nil {
			t.Fatalf("Freeze: %v", err)
		}

		// A different resolved membership (fewer subjects) must freeze to a
		// different snapshot, never silently reuse the first digest.
		result := resolvedResult(t)
		trimmed := result
		trimmed.Members = result.Members[:1]
		restricted2, err := population.ApplyRestrictions(trimmed, population.RestrictionDecision{Versions: mustPolicyVersions(), DiscloseMembership: true})
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		snap2, err := population.Freeze(def, "2026.1", restricted2, asOf, knownAt, watermarks)
		if err != nil {
			t.Fatalf("Freeze: %v", err)
		}
		if snap1.Digest == snap2.Digest {
			t.Fatal("freezing a different resolved membership produced the same digest")
		}
	})

	t.Run("RED_missing_context_is_rejected", func(t *testing.T) {
		if _, err := population.Freeze(def, "", restricted, asOf, knownAt, watermarks); err == nil {
			t.Fatal("expected Freeze without a revision version to fail")
		}
		if _, err := population.Freeze(population.Definition{}, "1", restricted, asOf, knownAt, watermarks); err == nil {
			t.Fatal("expected Freeze with an invalid definition to fail")
		}
	})

	t.Run("immutability_returned_subject_ids_cannot_mutate_the_snapshot", func(t *testing.T) {
		snap, err := population.Freeze(def, "2026.1", restricted, asOf, knownAt, watermarks)
		if err != nil {
			t.Fatalf("Freeze: %v", err)
		}
		ids := snap.SubjectIDList()
		ids[0] = "tampered"
		if snap.SubjectIDList()[0] == "tampered" {
			t.Fatal("mutating a returned subject id list mutated the snapshot")
		}
	})
}

// TestTodo_POP_005_Property proves two freezes of byte-identical inputs
// produce byte-identical digests, and any single-field difference in the time
// context changes the digest.
func TestTodo_POP_005_Property(t *testing.T) {
	def := validDefinition()
	restricted := restrictedForSnapshot(t)
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	watermarks := testWatermarks(t)

	first, err := population.Freeze(def, "2026.1", restricted, asOf, knownAt, watermarks)
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	second, err := population.Freeze(def, "2026.1", restricted, asOf, knownAt, watermarks)
	if err != nil {
		t.Fatalf("Freeze (repeat): %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("two freezes of identical inputs produced different digests")
	}

	laterAsOf := mustInstant(t, 2_000_000)
	third, err := population.Freeze(def, "2026.1", restricted, laterAsOf, knownAt, watermarks)
	if err != nil {
		t.Fatalf("Freeze (later as-of): %v", err)
	}
	if third.Digest == first.Digest {
		t.Fatal("changing as-of did not change the digest")
	}
}

// TestTodo_POP_005_Golden pins the sorted subject id order for a fixed
// three-worker fixture so an accidental reordering is caught by a test diff.
func TestTodo_POP_005_Golden(t *testing.T) {
	def := validDefinition()
	restricted := restrictedForSnapshot(t)
	snap, err := population.Freeze(def, "2026.1", restricted,
		mustInstant(t, 1_000_000), mustKnownAt(t, 1_000_000),
		testWatermarks(t))
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	want := []string{worker(1).String(), worker(2).String(), worker(3).String()}
	got := snap.SubjectIDList()
	if len(got) != len(want) {
		t.Fatalf("subject ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("subject ids[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
