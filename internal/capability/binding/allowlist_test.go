package binding

import (
	"sort"
	"strings"
	"testing"
)

// TestAllowlistIsWellFormed: an allowlist entry without an owning todo or a
// rationale is an excuse rather than a plan, and a duplicate id would let
// one gap be "closed" twice.
func TestAllowlistIsWellFormed(t *testing.T) {
	entries := Allowlist()
	if len(entries) == 0 {
		t.Fatal("the allowlist is empty; the live tree has known gaps and this check would be vacuous")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.GapID] {
			t.Errorf("duplicate allowlist entry for %s", e.GapID)
		}
		seen[e.GapID] = true
		if e.OwnerTodo == "" {
			t.Errorf("allowlist entry %s has no owning todo", e.GapID)
		}
		if e.Rationale == "" {
			t.Errorf("allowlist entry %s has no rationale", e.GapID)
		}
		kind, _, ok := strings.Cut(e.GapID, "|")
		if !ok || !GapKind(kind).Valid() {
			t.Errorf("allowlist entry %q does not begin with a declared gap kind", e.GapID)
		}
	}
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].GapID < entries[j].GapID }) {
		t.Error("Allowlist() is not sorted by gap id")
	}
	if got := len(AllowlistByGapID()); got != len(entries) {
		t.Errorf("AllowlistByGapID has %d entries, Allowlist has %d", got, len(entries))
	}
}

// TestAllowlistOwnersAreRealTodoIDs keeps an owner from degrading into a
// free-text note.
func TestAllowlistOwnersAreRealTodoIDs(t *testing.T) {
	known := map[string]bool{ownerBind001: true, ownerProto010: true, ownerEndpoint009: true}
	for _, e := range Allowlist() {
		if !known[e.OwnerTodo] {
			t.Errorf("allowlist entry %s names owner %q, which is not one of the declared owning todos", e.GapID, e.OwnerTodo)
		}
	}
}

// TestSharedHandlerAllowlistIsEmpty states the current fact explicitly: no
// typed symbol is claimed by two capabilities today, and the conformance
// check proves it stays that way.
func TestSharedHandlerAllowlistIsEmpty(t *testing.T) {
	if got := len(sharedHandlerAllowlist()); got != 0 {
		t.Fatalf("the shared-handler allowlist has %d entries; it is pinned empty", got)
	}
	table := liveTable(t)
	if got := len(table.GapsOfKind(GapHandlerBoundTwice)); got != 0 {
		t.Errorf("the live tree has %d handler symbols claimed by two capabilities; the empty allowlist is stale", got)
	}
}

// TestCheckConformanceSeparatesNewFromStale is the ratchet's own test: a
// gap the allowlist does not own is new, and an allowlist entry with no gap
// is stale. Both are failures; neither may be silently absorbed.
func TestCheckConformanceSeparatesNewFromStale(t *testing.T) {
	table := Table{Gaps: []Gap{
		{Kind: GapNoHandler, Capability: "known", Detail: "known gap"},
		{Kind: GapNoHandler, Capability: "surprise", Detail: "new gap"},
	}}
	allowlist := []AllowlistEntry{
		{GapID: Gap{Kind: GapNoHandler, Capability: "known"}.ID(), OwnerTodo: "T-1", Rationale: "r"},
		{GapID: Gap{Kind: GapNoHandler, Capability: "closed"}.ID(), OwnerTodo: "T-2", Rationale: "r"},
	}

	report := CheckConformance(table, allowlist)
	if report.OK() {
		t.Fatal("a report with a new gap and a stale entry reported OK")
	}
	if len(report.Accepted) != 1 || report.Accepted[0].Capability != "known" {
		t.Errorf("accepted = %+v, want exactly the known gap", report.Accepted)
	}
	if len(report.NewGaps) != 1 || report.NewGaps[0].Capability != "surprise" {
		t.Errorf("new gaps = %+v, want exactly the surprise gap", report.NewGaps)
	}
	if len(report.StaleAllowlist) != 1 || !strings.Contains(report.StaleAllowlist[0].GapID, "closed") {
		t.Errorf("stale entries = %+v, want exactly the closed one", report.StaleAllowlist)
	}

	explain := report.Explain()
	for _, want := range []string{"1 accepted, 1 new, 1 stale", "NEW   NO_HANDLER|surprise|", "STALE NO_HANDLER|closed|"} {
		if !strings.Contains(explain, want) {
			t.Errorf("Explain() is missing %q:\n%s", want, explain)
		}
	}
}

func TestCheckConformanceOnAnExactMatch(t *testing.T) {
	gap := Gap{Kind: GapNoWireMethod, Capability: "c", Detail: "d"}
	report := CheckConformance(
		Table{Gaps: []Gap{gap}},
		[]AllowlistEntry{{GapID: gap.ID(), OwnerTodo: "T-1", Rationale: "r"}},
	)
	if !report.OK() {
		t.Fatalf("an exact match did not report OK:\n%s", report.Explain())
	}
	if len(report.Accepted) != 1 {
		t.Errorf("accepted %d gaps, want 1", len(report.Accepted))
	}
}

// TestAllowlistOnlyCoversGapsThatCanExist: an allowlist id whose gap kind
// or subject could never be produced would be dead weight that silently
// never turns.
func TestAllowlistOnlyCoversGapsThatCanExist(t *testing.T) {
	table := liveTable(t)
	live := map[string]bool{}
	for _, id := range table.GapIDs() {
		live[id] = true
	}
	for _, e := range Allowlist() {
		if !live[e.GapID] {
			t.Errorf("allowlist entry %s matches no gap in the live tree; remove it", e.GapID)
		}
	}
	if len(Allowlist()) != len(table.GapIDs()) {
		t.Errorf("the allowlist has %d entries and the live tree has %d distinct gaps; they must be set-equal",
			len(Allowlist()), len(table.GapIDs()))
	}
}
