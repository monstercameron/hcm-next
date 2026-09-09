package queryplans_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/queryplans"
)

// TestCatalog_EveryEntryIsWellFormed is a pure Go-level guard, run with no
// database: every declared entry names itself, its owner, its table and at
// least one expected index, and carries a Prepare function -- the shape
// prover_test.go's database-backed tests assume without re-checking it.
func TestCatalog_EveryEntryIsWellFormed(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, e := range queryplans.Catalog() {
		if e.Name == "" {
			t.Fatalf("entry with owner %q has no Name", e.Owner)
		}
		if seen[e.Name] {
			t.Errorf("duplicate entry name %q", e.Name)
		}
		seen[e.Name] = true
		if e.Owner == "" {
			t.Errorf("%s: Owner is empty", e.Name)
		}
		if e.Table == "" {
			t.Errorf("%s: Table is empty", e.Name)
		}
		if len(e.ExpectedIndexSubstrings) == 0 {
			t.Errorf("%s: ExpectedIndexSubstrings is empty", e.Name)
		}
		for _, s := range e.ExpectedIndexSubstrings {
			if s == "" {
				t.Errorf("%s: ExpectedIndexSubstrings contains an empty string", e.Name)
			}
		}
		if e.Prepare == nil {
			t.Errorf("%s: Prepare is nil", e.Name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("Catalog() returned no entries")
	}
}

// TestCatalog_DefaultRowThresholdIsPastTheSeqScanCrossover pins
// DefaultRowThreshold's own documented floor: small enough tables are
// correctly seq-scanned by the planner, so a proof run below a few hundred
// rows would not distinguish "no index" from "table too small to bother".
func TestCatalog_DefaultRowThresholdIsPastTheSeqScanCrossover(t *testing.T) {
	t.Parallel()
	if queryplans.DefaultRowThreshold < 1000 {
		t.Errorf("DefaultRowThreshold = %d, want at least 1000 to be a meaningful proof of index usage",
			queryplans.DefaultRowThreshold)
	}
}
