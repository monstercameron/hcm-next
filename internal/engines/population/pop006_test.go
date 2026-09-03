package population_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/population"
)

func snapshotWithMembers(t *testing.T, memberCount int) population.Snapshot {
	t.Helper()
	result := resolvedResult(t)
	trimmed := result
	trimmed.Members = result.Members[:memberCount]
	restricted, err := population.ApplyRestrictions(trimmed, population.RestrictionDecision{
		Versions: mustPolicyVersions(), DiscloseMembership: true,
	})
	if err != nil {
		t.Fatalf("ApplyRestrictions: %v", err)
	}
	snap, err := population.Freeze(validDefinition(), "2026.1", restricted,
		mustInstant(t, 1_000_000), mustKnownAt(t, 1_000_000), testWatermarks(t))
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return snap
}

func protectedSnapshot(t *testing.T) population.Snapshot {
	t.Helper()
	result := resolvedResult(t)
	restricted, err := population.ApplyRestrictions(result, population.RestrictionDecision{Versions: mustPolicyVersions()})
	if err != nil {
		t.Fatalf("ApplyRestrictions: %v", err)
	}
	snap, err := population.Freeze(validDefinition(), "2026.1", restricted,
		mustInstant(t, 1_000_000), mustKnownAt(t, 1_000_000), testWatermarks(t))
	if err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return snap
}

// TestTodo_POP_006 proves the RED and GREEN clauses of planning/todos.md
// POP-006: every subject in either snapshot lands in exactly one of
// added/removed/unchanged with no double counting, and an unauthorized
// (protected) snapshot pair discloses neither members nor a count delta.
func TestTodo_POP_006(t *testing.T) {
	t.Run("RED_no_double_count_across_partitions", func(t *testing.T) {
		from := snapshotWithMembers(t, 2) // workers 1, 2
		to := snapshotWithMembers(t, 3)   // workers 1, 2, 3
		diff, err := population.Diff(from, to)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		seen := map[string]int{}
		for _, id := range diff.Added {
			seen[id]++
		}
		for _, id := range diff.Removed {
			seen[id]++
		}
		for _, id := range diff.Unchanged {
			seen[id]++
		}
		for id, n := range seen {
			if n != 1 {
				t.Fatalf("subject %s appeared in %d partitions, want exactly 1", id, n)
			}
		}
		if len(diff.Added) != 1 || diff.Added[0] != worker(3).String() {
			t.Fatalf("added = %v, want [%s]", diff.Added, worker(3))
		}
		if len(diff.Removed) != 0 {
			t.Fatalf("removed = %v, want none", diff.Removed)
		}
		if len(diff.Unchanged) != 2 {
			t.Fatalf("unchanged = %v, want 2", diff.Unchanged)
		}
	})

	t.Run("GREEN_unauthorized_diff_leaks_neither_members_nor_count", func(t *testing.T) {
		from := protectedSnapshot(t)
		to := snapshotWithMembers(t, 3)
		diff, err := population.Diff(from, to)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		if !diff.Protected {
			t.Fatal("a diff involving a protected snapshot did not mark itself Protected")
		}
		if diff.Added != nil || diff.Removed != nil || diff.Unchanged != nil {
			t.Fatal("a protected diff disclosed a member partition")
		}
		if diff.CountDelta.IsValue() {
			t.Fatal("a protected diff disclosed a count delta")
		}
	})

	t.Run("RED_diffing_different_definitions_is_rejected", func(t *testing.T) {
		from := snapshotWithMembers(t, 2)
		other := validDefinition()
		other.ID = "a-different-population"
		restricted, err := population.ApplyRestrictions(resolvedResult(t), population.RestrictionDecision{Versions: mustPolicyVersions(), DiscloseMembership: true})
		if err != nil {
			t.Fatalf("ApplyRestrictions: %v", err)
		}
		to, err := population.Freeze(other, "1", restricted, mustInstant(t, 1_000_000), mustKnownAt(t, 1_000_000), testWatermarks(t))
		if err != nil {
			t.Fatalf("Freeze: %v", err)
		}
		if _, err := population.Diff(from, to); !errors.Is(err, population.ErrDiffDefinitionMismatch) {
			t.Fatalf("error = %v, want ErrDiffDefinitionMismatch", err)
		}
	})

	t.Run("GREEN_no_change_diff_is_empty_and_deterministic", func(t *testing.T) {
		snap := snapshotWithMembers(t, 3)
		diff, err := population.Diff(snap, snap)
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		if len(diff.Added) != 0 || len(diff.Removed) != 0 || len(diff.Unchanged) != 3 {
			t.Fatalf("diffing a snapshot against itself: added=%v removed=%v unchanged=%v", diff.Added, diff.Removed, diff.Unchanged)
		}
		delta, ok := diff.CountDelta.Get()
		if !ok || delta != 0 {
			t.Fatalf("count delta = %v (ok=%v), want 0", delta, ok)
		}
	})
}

// TestTodo_POP_006_Property proves |Added| - |Removed| always equals the
// exact difference in disclosed count between the two snapshots, for several
// from/to combinations.
func TestTodo_POP_006_Property(t *testing.T) {
	sizes := []struct{ from, to int }{{0, 3}, {1, 3}, {3, 1}, {2, 2}, {3, 0}}
	for _, sz := range sizes {
		from := snapshotWithMembers(t, sz.from)
		to := snapshotWithMembers(t, sz.to)
		diff, err := population.Diff(from, to)
		if err != nil {
			t.Fatalf("Diff(%d,%d): %v", sz.from, sz.to, err)
		}
		gotDelta := len(diff.Added) - len(diff.Removed)
		wantDelta := sz.to - sz.from
		if gotDelta != wantDelta {
			t.Fatalf("Diff(%d,%d): added-removed = %d, want %d", sz.from, sz.to, gotDelta, wantDelta)
		}
		if len(diff.Added)+len(diff.Unchanged) != sz.to {
			t.Fatalf("Diff(%d,%d): added+unchanged = %d, want %d (to size)", sz.from, sz.to, len(diff.Added)+len(diff.Unchanged), sz.to)
		}
		if len(diff.Removed)+len(diff.Unchanged) != sz.from {
			t.Fatalf("Diff(%d,%d): removed+unchanged = %d, want %d (from size)", sz.from, sz.to, len(diff.Removed)+len(diff.Unchanged), sz.from)
		}
	}
}

// TestTodo_POP_006_Golden pins the exact partition for the 2-to-3-member
// fixture transition.
func TestTodo_POP_006_Golden(t *testing.T) {
	from := snapshotWithMembers(t, 2)
	to := snapshotWithMembers(t, 3)
	diff, err := population.Diff(from, to)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diff.Added) != 1 || diff.Added[0] != worker(3).String() {
		t.Fatalf("added = %v, want [%s]", diff.Added, worker(3))
	}
	wantUnchanged := []string{worker(1).String(), worker(2).String()}
	if len(diff.Unchanged) != 2 || diff.Unchanged[0] != wantUnchanged[0] || diff.Unchanged[1] != wantUnchanged[1] {
		t.Fatalf("unchanged = %v, want %v", diff.Unchanged, wantUnchanged)
	}
}

// TestTodo_POP_006_Security proves that even when only one side of the diff
// is protected, no partition or count delta is disclosed: partial protection
// is treated the same as full protection.
func TestTodo_POP_006_Security(t *testing.T) {
	authorized := snapshotWithMembers(t, 3)
	protected := protectedSnapshot(t)

	fromProtected, err := population.Diff(protected, authorized)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	toProtected, err := population.Diff(authorized, protected)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, d := range []population.SnapshotDiff{fromProtected, toProtected} {
		if !d.Protected || d.Added != nil || d.Removed != nil || d.Unchanged != nil || d.CountDelta.IsValue() {
			t.Fatalf("a diff with one protected side leaked: %+v", d)
		}
	}
}
