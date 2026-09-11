package designclosure

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func closureFixture() (accepted []string, snap Snapshot) {
	accepted = []string{"intent/a/v1", "intent/b/v1", "intent/c/v1", "intent/d/v1", "intent/e/v1", "intent/f/v1"}
	snap = Snapshot{
		Items: []ItemInput{
			{ID: "intent/a/v1", Source: "catalog.yaml#intent/a/v1", Owner: "family-one", Phase: "Phase 1"},
			{ID: "intent/b/v1", Source: "catalog.yaml#intent/b/v1", Owner: "family-one", Phase: "Phase 1"},
			{ID: "intent/c/v1", Source: "catalog.yaml#intent/c/v1", Owner: "family-two", Phase: "Phase 1"},
			{ID: "intent/d/v1", Source: "catalog.yaml#intent/d/v1", Owner: "family-two", Phase: "Phase 1"},
			{ID: "intent/e/v1", Source: "catalog.yaml#intent/e/v1", Owner: "family-one", Phase: "Phase 1"},
			{ID: "intent/f/v1", Source: "", Owner: "family-one", Phase: "Phase 1"},
		},
		Todos: []TodoInput{
			{ID: "DONE-1", Phase: "P0", Done: true, Direct: []string{"intent/a/v1"}, TestNames: []string{"TestDone"}, EvidenceTests: []string{"TestDone"}, EvidenceDigest: "digest-a"},
			{ID: "OPEN-1", Phase: "P0", Done: false, Direct: []string{"intent/b/v1"}, TestNames: []string{"TestOpen"}, EvidenceTests: []string{"TestOpen"}, EvidenceDigest: ""},
			{ID: "OPEN-2", Phase: "GATE_A", Done: false, Direct: []string{"intent/b/v1"}, TestNames: []string{"TestOpen"}, EvidenceTests: []string{"TestMissing"}, EvidenceDigest: ""},
			{ID: "BARE-1", Phase: "P0", Done: true, Direct: []string{"intent/c/v1"}, TestNames: []string{"TestBare"}, EvidenceTests: nil, EvidenceDigest: ""},
			{ID: "OLD-1", Phase: "P0", Done: false, Retired: true, Direct: []string{"intent/d/v1"}, TestNames: []string{"TestOld"}, EvidenceTests: []string{"TestOld"}, EvidenceDigest: ""},
		},
		Workflows: map[string][]string{
			"intent/a/v1": {"flow-a"},
			"intent/b/v1": {"flow-b"},
			"intent/d/v1": {"flow-d"},
		},
		OpenGaps: map[string][]string{
			"intent/b/v1": {"feature-x"},
		},
		Deferrals:  map[string]Deferral{},
		Rejections: map[string]string{},
		Selections: map[string]string{},
		TestExists: map[string]bool{"TestDone": true, "TestOpen": true, "TestBare": true},
	}
	return accepted, snap
}

func rowsByID(register Register) map[string]Row {
	out := make(map[string]Row)
	for _, row := range register.Rows {
		out[row.Item] = row
	}
	return out
}

func findingCodes(findings []Finding, item string) map[string]int {
	codes := make(map[string]int)
	for _, finding := range findings {
		if finding.Item == item {
			codes[finding.Code]++
		}
	}
	return codes
}

// TestDesignClosureRegisterNamesEveryRequiredDecisionOwnerArtifactAndGate
// is the CLOSE-001 primary oracle: every row carries a decision, an owner
// and a gate; missing dimensions are exact findings; totals reconcile.
func TestDesignClosureRegisterNamesEveryRequiredDecisionOwnerArtifactAndGate(t *testing.T) {
	accepted, snap := closureFixture()
	register, findings := Compile(accepted, snap)
	rows := rowsByID(register)
	if len(rows) != len(accepted) {
		t.Fatalf("rows = %d, want %d (every accepted item joined)", len(rows), len(accepted))
	}
	for id, row := range rows {
		if row.Decision == "" {
			t.Errorf("%s has no decision state", id)
		}
		if row.Owner == "" {
			t.Errorf("%s has no owner", id)
		}
		if row.Gate == "" {
			t.Errorf("%s has no gate", id)
		}
	}
	if got := rows["intent/a/v1"].Decision; got != StateVerified {
		t.Errorf("intent/a/v1 decision = %s, want VERIFIED", got)
	}
	if got := rows["intent/b/v1"].Decision; got != StateDesigned {
		t.Errorf("intent/b/v1 decision = %s, want DESIGNED", got)
	}
	if got := rows["intent/c/v1"].Decision; got != StateContracted {
		t.Errorf("intent/c/v1 decision = %s, want CONTRACTED (prose completion caps the ladder)", got)
	}
	if got := rows["intent/d/v1"].Decision; got != StateUnbound {
		t.Errorf("intent/d/v1 decision = %s, want UNBOUND (only a retired todo binds it)", got)
	}
	if got := rows["intent/e/v1"].Decision; got != StateUnselected {
		t.Errorf("intent/e/v1 decision = %s, want UNSELECTED", got)
	}
	if codes := findingCodes(findings, "intent/c/v1"); codes[TickWithoutEvidence] == 0 {
		t.Errorf("intent/c/v1 prose completion accepted: %+v", findings)
	}
	if codes := findingCodes(findings, "intent/b/v1"); codes[MissingTest] == 0 {
		t.Errorf("intent/b/v1 dangling evidence test accepted: %+v", findings)
	}
	if codes := findingCodes(findings, "intent/f/v1"); codes[MissingSource] == 0 {
		t.Errorf("intent/f/v1 missing source accepted: %+v", findings)
	}
	blockers := rows["intent/b/v1"].Blockers
	if len(blockers) == 0 {
		t.Fatal("intent/b/v1 names no blockers")
	}
	seenTodo, seenGap := false, false
	for _, blocker := range blockers {
		if blocker.Kind == "todo" && blocker.Ref == "OPEN-1" && blocker.Gate == "P0" {
			seenTodo = true
		}
		if blocker.Kind == "gap" && blocker.Ref == "feature-x" {
			seenGap = true
		}
		if blocker.Gate == "" {
			t.Errorf("blocker %+v names no gate", blocker)
		}
	}
	if !seenTodo || !seenGap {
		t.Errorf("intent/b/v1 blockers miss the open todo or gap: %+v", blockers)
	}
	sum := 0
	for _, count := range register.Totals.ByState {
		sum += count
	}
	if sum != register.Totals.Rows || sum != len(accepted) {
		t.Errorf("totals reconcile to %d/%d over %d accepted: no row may disappear", sum, register.Totals.Rows, len(accepted))
	}
	if register.Totals.Digest == "" {
		t.Error("register carries no digest")
	}
	again, _ := Compile(accepted, snap)
	if again.Totals.Digest != register.Totals.Digest {
		t.Error("register digest not deterministic")
	}
}

func TestTodo_CLOSE_001_Property(t *testing.T) {
	t.Run("partial progress contracts", func(t *testing.T) {
		accepted, snap := closureFixture()
		snap.Todos = append(snap.Todos, TodoInput{ID: "DONE-2", Phase: "P0", Done: true, Direct: []string{"intent/b/v1"}, TestNames: []string{"TestOpen"}, EvidenceTests: []string{"TestOpen"}, EvidenceDigest: "digest-b"})
		register, _ := Compile(accepted, snap)
		if got := rowsByID(register)["intent/b/v1"].Decision; got != StateContracted {
			t.Errorf("decision = %s, want CONTRACTED", got)
		}
	})
	t.Run("implemented needs every todo done", func(t *testing.T) {
		accepted, snap := closureFixture()
		snap.Todos = []TodoInput{
			{ID: "DONE-1", Phase: "P0", Done: true, Direct: []string{"intent/a/v1"}, TestNames: []string{"TestDone"}, EvidenceTests: []string{"TestDone"}, EvidenceDigest: "digest-a"},
			{ID: "DONE-3", Phase: "P0", Done: true, Direct: []string{"intent/a/v1"}, TestNames: []string{"TestDone"}, EvidenceTests: []string{"TestGhost"}, EvidenceDigest: "digest-a2"},
		}
		register, findings := Compile(accepted, snap)
		rows := rowsByID(register)
		if got := rows["intent/a/v1"].Decision; got != StateImplemented {
			t.Errorf("decision = %s, want IMPLEMENTED (dangling test caps verification)", got)
		}
		if codes := findingCodes(findings, "intent/a/v1"); codes[MissingTest] == 0 {
			t.Errorf("dangling TestGhost accepted: %+v", findings)
		}
		if len(rows["intent/a/v1"].Blockers) == 0 {
			t.Error("IMPLEMENTED row without a test blocker names nothing to close")
		}
	})
	t.Run("deferred needs expiry", func(t *testing.T) {
		accepted, snap := closureFixture()
		snap.Deferrals["intent/e/v1"] = Deferral{Rationale: "awaiting selection", Expiry: ""}
		_, findings := Compile(accepted, snap)
		if codes := findingCodes(findings, "intent/e/v1"); codes[MissingExpiry] == 0 {
			t.Errorf("expiry-free deferral accepted: %+v", findings)
		}
	})
	t.Run("deferred with expiry states why", func(t *testing.T) {
		accepted, snap := closureFixture()
		snap.Deferrals["intent/e/v1"] = Deferral{Rationale: "awaiting selection", Expiry: "2026-12-31"}
		register, findings := Compile(accepted, snap)
		rows := rowsByID(register)
		if got := rows["intent/e/v1"].Decision; got != StateDeferred {
			t.Errorf("decision = %s, want DEFERRED", got)
		}
		if codes := findingCodes(findings, "intent/e/v1"); codes[MissingExpiry] > 0 {
			t.Errorf("recorded expiry still flagged: %+v", findings)
		}
		if rows["intent/e/v1"].Expiry != "2026-12-31" {
			t.Errorf("expiry = %q, want the recorded date", rows["intent/e/v1"].Expiry)
		}
	})
	t.Run("rejected names its rationale", func(t *testing.T) {
		accepted, snap := closureFixture()
		snap.Rejections["intent/e/v1"] = "out of charter"
		register, _ := Compile(accepted, snap)
		rows := rowsByID(register)
		if got := rows["intent/e/v1"].Decision; got != StateRejected {
			t.Errorf("decision = %s, want REJECTED", got)
		}
		if len(rows["intent/e/v1"].Blockers) == 0 {
			t.Error("REJECTED row without a rationale blocker hides why")
		}
	})
	t.Run("rejection beats deferral", func(t *testing.T) {
		accepted, snap := closureFixture()
		snap.Deferrals["intent/e/v1"] = Deferral{Rationale: "awaiting selection", Expiry: "2026-12-31"}
		snap.Rejections["intent/e/v1"] = "out of charter"
		register, _ := Compile(accepted, snap)
		if got := rowsByID(register)["intent/e/v1"].Decision; got != StateRejected {
			t.Errorf("decision = %s, want REJECTED", got)
		}
	})
	t.Run("missing owner and phase fail", func(t *testing.T) {
		accepted, snap := closureFixture()
		snap.Items = append(snap.Items, ItemInput{ID: "intent/g/v1"})
		register, findings := Compile(append(accepted, "intent/g/v1"), snap)
		rows := rowsByID(register)
		if got := rows["intent/g/v1"].Decision; got != StateUnselected {
			t.Errorf("decision = %s, want UNSELECTED", got)
		}
		codes := findingCodes(findings, "intent/g/v1")
		for _, want := range []string{MissingSource, MissingOwner, MissingPhase} {
			if codes[want] == 0 {
				t.Errorf("want %s, got %+v", want, codes)
			}
		}
	})
}

func TestTodo_CLOSE_001_Golden(t *testing.T) {
	accepted, snap := closureFixture()
	register, findings := Compile(accepted, snap)
	got, err := MarshalReport(register, findings)
	if err != nil {
		t.Fatal(err)
	}
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_CLOSE_001_Security(t *testing.T) {
	accepted, snap := closureFixture()
	t.Run("duplicate items cannot merge rows", func(t *testing.T) {
		dup := snap
		dup.Items = append(append([]ItemInput(nil), snap.Items...), snap.Items[0])
		register, findings := Compile(accepted, dup)
		if codes := findingCodes(findings, "intent/a/v1"); codes[DuplicateItem] == 0 {
			t.Fatalf("duplicate item accepted: %+v", findings)
		}
		if len(rowsByID(register)) != len(accepted) {
			t.Fatalf("duplicate item grew the register to %d rows", len(register.Rows))
		}
	})
	t.Run("unknown items stay visible", func(t *testing.T) {
		rogue := snap
		rogue.Items = append(append([]ItemInput(nil), snap.Items...), ItemInput{ID: "intent/rogue/v1", Source: "nowhere", Owner: "nobody", Phase: "Phase 9"})
		register, findings := Compile(accepted, rogue)
		if codes := findingCodes(findings, "intent/rogue/v1"); codes[UnknownItem] == 0 {
			t.Fatalf("unknown item accepted silently: %+v", findings)
		}
		if _, ok := rowsByID(register)["intent/rogue/v1"]; !ok {
			t.Fatal("unknown item disappeared from totals")
		}
		if got := register.Totals.Rows; got != len(accepted)+1 {
			t.Fatalf("totals rows = %d, want every row counted", got)
		}
	})
	t.Run("dropped items break the digest", func(t *testing.T) {
		full, _ := Compile(accepted, snap)
		trimmed := snap
		trimmed.Items = append([]ItemInput(nil), snap.Items[:len(snap.Items)-1]...)
		short, shortFindings := Compile(accepted, trimmed)
		if short.Totals.Digest == full.Totals.Digest {
			t.Fatal("dropped accepted item left the digest unchanged")
		}
		if codes := findingCodes(shortFindings, "intent/f/v1"); codes[IncompleteCoverage] == 0 {
			t.Fatalf("dropped item not reported: %+v", shortFindings)
		}
		if _, ok := rowsByID(short)["intent/f/v1"]; ok {
			t.Fatal("dropped item still produced a row")
		}
	})
	t.Run("forged states cannot enter", func(t *testing.T) {
		forged := snap
		forged.Todos = []TodoInput{{ID: "LIAR-1", Phase: "P0", Done: true, Direct: []string{"intent/e/v1"}, TestNames: []string{"TestLiar"}, EvidenceTests: nil, EvidenceDigest: ""}}
		register, findings := Compile(accepted, forged)
		rows := rowsByID(register)
		if got := rows["intent/e/v1"].Decision; got == StateVerified || got == StateImplemented {
			t.Fatalf("evidence-free tick reached %s", got)
		}
		if codes := findingCodes(findings, "intent/e/v1"); codes[TickWithoutEvidence] == 0 {
			t.Fatalf("prose completion reached CONTRACTED silently: %+v", findings)
		}
	})
}

func TestTodo_CLOSE_001_Conformance(t *testing.T) {
	root := repoRoot(t)
	snap, accepted, err := LoadSnapshot(root)
	if err != nil {
		t.Fatalf("load live snapshot: %v", err)
	}
	if len(accepted) == 0 {
		t.Fatal("live snapshot names no accepted intents")
	}
	first, firstFindings := Compile(accepted, snap)
	second, _ := Compile(accepted, snap)
	if first.Totals.Digest != second.Totals.Digest {
		t.Fatal("live register digest not deterministic")
	}
	seen := make(map[string]int)
	for _, row := range first.Rows {
		seen[row.Item]++
		if !validDecision(row.Decision) {
			t.Errorf("row %s carries unknown decision %q", row.Item, row.Decision)
		}
		if row.Gate == "" {
			t.Errorf("row %s names no gate", row.Item)
		}
	}
	for item, count := range seen {
		if count != 1 {
			t.Errorf("item %s appears %d times", item, count)
		}
	}
	if len(first.Rows) != len(accepted) {
		t.Errorf("rows = %d over %d accepted: scope items may not disappear", len(first.Rows), len(accepted))
	}
	counts := make(map[DecisionState]int)
	for _, row := range first.Rows {
		counts[row.Decision]++
	}
	codeCounts := make(map[string]int)
	for _, finding := range firstFindings {
		codeCounts[finding.Code]++
	}
	t.Logf("live design closure: %d rows %v findings %v", len(first.Rows), counts, codeCounts)
}

func TestTodo_CLOSE_001_Mutation(t *testing.T) {
	accepted, snap := closureFixture()
	base, _ := Compile(accepted, snap)
	baseDigest := base.Totals.Digest
	t.Run("unticking falls off verified", func(t *testing.T) {
		mutated := snap
		mutated.Todos = []TodoInput{{ID: "DONE-1", Phase: "P0", Done: false, Direct: []string{"intent/a/v1"}, TestNames: []string{"TestDone"}, EvidenceTests: []string{"TestDone"}, EvidenceDigest: "digest-a"}}
		register, _ := Compile(accepted, mutated)
		if got := rowsByID(register)["intent/a/v1"].Decision; got != StateDesigned {
			t.Errorf("decision = %s, want DESIGNED after unticking", got)
		}
		if register.Totals.Digest == baseDigest {
			t.Error("untick left the digest unchanged")
		}
	})
	t.Run("losing the artifact is reported", func(t *testing.T) {
		mutated := snap
		mutated.Workflows = map[string][]string{}
		_, findings := Compile(accepted, mutated)
		if codes := findingCodes(findings, "intent/a/v1"); codes[MissingArtifact] == 0 {
			t.Errorf("lost workflow accepted: %+v", findings)
		}
	})
	t.Run("losing evidence is reported", func(t *testing.T) {
		mutated := snap
		mutated.Todos = []TodoInput{{ID: "DONE-1", Phase: "P0", Done: true, Direct: []string{"intent/a/v1"}, TestNames: []string{"TestDone"}, EvidenceTests: []string{"TestDone"}, EvidenceDigest: ""}}
		_, findings := Compile(accepted, mutated)
		if codes := findingCodes(findings, "intent/a/v1"); codes[MissingEvidence] == 0 {
			t.Errorf("lost evidence accepted: %+v", findings)
		}
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}
