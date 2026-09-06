package todogovernance

import (
	"strings"
	"testing"
)

const fixtureFooter = `  - **INTENT CONTEXT:** ` + "`ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`" + `.
  - **TEST:** ` + "`Test%[1]s`" + `.
  - **TEST MATRIX:** ` + "`PRIMARY=Test%[1]s`" + `.
  - **RED:** fixture red.
  - **GREEN:** fixture green.
  - **REFACTOR:** fixture refactor.
  - **Refs:** [fixture](fixture.md).
`

// fixtureTodo renders one well-formed todo block (everything GOV-017 needs
// present) with the given id, phase and Depends text, so dependency fixtures
// can focus on the one field under test.
func fixtureTodo(id, phase, depends string) string {
	return "- [ ] `" + id + "` **[" + phase + "][LUNA] Fixture todo.**\n" +
		"  - **Depends:** " + depends + ".\n" +
		strings.ReplaceAll(fixtureFooter, "%[1]s", strings.ReplaceAll(id, "-", ""))
}

// TestTodo_GOV_016 is the PRIMARY test declared by `GOV-016` in
// planning/todos.md. It proves ValidateDependencies rejects an unknown ID,
// prose mixed into a Depends field, a dependency cycle and a phase
// inversion with the exact offending todo ID and code, then runs the same
// validator against the real registry and markdown and requires every
// finding to already be in the reviewed gov016Allowlist (see allowlist.go)
// - so today's known backlog defects stay green while any NEW one fails.
func TestTodo_GOV_016(t *testing.T) {
	t.Run("UnresolvedDependency", func(t *testing.T) {
		content := fixtureTodo("FX-001", "P0", "`FX-999`")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))
		requireFinding(t, findings, "FX-001", "GOV-016", CodeUnresolvedDependency)
	})

	t.Run("ProseDependency", func(t *testing.T) {
		content := fixtureTodo("FX-001", "P0", "none") +
			fixtureTodo("FX-002", "P0", "`FX-001`, hostile-content ingress")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))
		requireFinding(t, findings, "FX-002", "GOV-016", CodeProseDependency)
		requireNoFinding(t, findings, "FX-001", "GOV-016", CodeProseDependency)
	})

	t.Run("DependencyCycle", func(t *testing.T) {
		content := fixtureTodo("FX-010", "P0", "`FX-011`") +
			fixtureTodo("FX-011", "P0", "`FX-010`")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))
		requireFinding(t, findings, "FX-010", "GOV-016", CodeDependencyCycle)
	})

	t.Run("PhaseInversion", func(t *testing.T) {
		content := fixtureTodo("FX-020", "P0", "`FX-021`") +
			fixtureTodo("FX-021", "GATE_A", "none")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))
		requireFinding(t, findings, "FX-020", "GOV-016", CodePhaseInversion)
	})

	t.Run("CleanGraphIsSilent", func(t *testing.T) {
		content := fixtureTodo("FX-030", "P0", "none") +
			fixtureTodo("FX-031", "GATE_A", "`FX-030`")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))
		if len(findings) != 0 {
			t.Errorf("expected no findings for a resolved, acyclic, phase-ordered graph, got: %v", findings)
		}
	})

	t.Run("RealCorpus", func(t *testing.T) {
		registryTodos := loadRealRegistry(t)
		records := loadRealMarkdown(t)
		findings := ValidateDependencies(registryTodos, RawDependsByID(records))
		assertOnlyAllowlisted(t, findings, gov016Allowlist)
	})
}

// TestTodo_GOV_016_Golden pins the exact finding set for one small,
// hand-checked fixture graph so a change to message wording, ordering or
// detection logic is caught even when it doesn't flip a pass/fail boundary.
func TestTodo_GOV_016_Golden(t *testing.T) {
	content := fixtureTodo("G-001", "P0", "`G-999`") +
		fixtureTodo("G-002", "P0", "`G-001`, stray prose") +
		fixtureTodo("G-003", "GATE_A", "`G-004`") +
		fixtureTodo("G-004", "GATE_B", "none")
	records, errs := ParseRecords(content)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	findings := ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))

	want := []string{
		"GOV-016|G-001|UNRESOLVED_DEPENDENCY|G-999",
		"GOV-016|G-002|PROSE_DEPENDENCY|",
		"GOV-016|G-003|PHASE_INVERSION|G-004",
	}
	got := make([]string, 0, len(findings))
	for _, f := range findings {
		got = append(got, f.Key())
	}
	if len(got) != len(want) {
		t.Fatalf("golden mismatch: want %d findings %v, got %d %v", len(want), want, len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("golden mismatch at %d: want %q, got %q (full: %v)", i, want[i], got[i], got)
		}
	}
}

// TestTodo_GOV_016_Property checks an invariant that must hold for any
// well-formed dependency graph regardless of its shape: a graph built only
// from edges between todos declared in strictly non-decreasing phase order
// never produces a PHASE_INVERSION finding, and a graph with no repeated
// node in any walk never produces a DEPENDENCY_CYCLE finding. Generated
// cases vary the chain length and phase spacing rather than using a
// property-testing library, since todoregistry has no generator dependency
// to reuse.
func TestTodo_GOV_016_Property(t *testing.T) {
	phases := []string{"P0", "GATE_A", "GATE_B", "GATE_C", "PHASE_2", "PHASE_3", "PHASE_4", "PHASE_5"}
	for chainLen := 1; chainLen <= len(phases); chainLen++ {
		var content strings.Builder
		ids := make([]string, chainLen)
		for i := 0; i < chainLen; i++ {
			ids[i] = "P" + phases[i]
		}
		for i, id := range ids {
			depends := "none"
			if i > 0 {
				// Each todo depends on the PREVIOUS (earlier-or-equal phase)
				// todo, so the chain is monotonically non-decreasing and
				// must never be flagged as a phase inversion.
				depends = "`" + ids[i-1] + "`"
			}
			content.WriteString(fixtureTodo(id, phases[i], depends))
		}
		records, errs := ParseRecords(content.String())
		if len(errs) != 0 {
			t.Fatalf("chainLen=%d: unexpected parse errors: %v", chainLen, errs)
		}
		findings := ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))
		for _, f := range findings {
			if f.Code == CodePhaseInversion || f.Code == CodeDependencyCycle {
				t.Errorf("chainLen=%d: monotonically-ordered acyclic chain produced %s: %v", chainLen, f.Code, f)
			}
		}
	}
}

// FuzzTodo_GOV_016 feeds arbitrary text into the raw-Depends prose detector
// (the hand-written token scanner in stripDependencyTokens) to prove it
// never panics on hostile input - backticks with no closing pair, nested
// punctuation, unicode dashes, empty tokens, and so on.
func FuzzTodo_GOV_016(f *testing.F) {
	seeds := []string{
		"", "none", "`A-001`", "`A-001`, `A-002`", "`A-001` - `A-005`",
		"`A-001", "A-001`", "`` `A`", "prose only", "`A-001`;`A-002`",
		"`A-001` to `A-010`", "—", "–", ",,,", "`" + strings.Repeat("x", 500) + "`",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("stripDependencyTokens panicked on %q: %v", raw, r)
			}
		}()
		_ = stripDependencyTokens(raw)

		// Also exercise the full validator end to end with the fuzzed text
		// as one todo's Depends field, guarding against a panic anywhere in
		// ParseTodos, BuildRecords or ValidateDependencies.
		content := fixtureTodo("FZ-001", "P0", safeDependsLiteral(raw))
		records, errs := ParseRecords(content)
		_ = errs
		_ = ValidateDependencies(TodosFromRecords(records), RawDependsByID(records))
	})
}

// safeDependsLiteral guards against the fuzzer picking a string containing
// a newline, which would corrupt the fixture's line structure rather than
// exercising the Depends parser.
func safeDependsLiteral(raw string) string {
	if strings.ContainsAny(raw, "\n\r") || raw == "" {
		return "none"
	}
	return raw
}
