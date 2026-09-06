package todogovernance

import (
	"strings"
	"testing"
)

// tddFixture renders one todo block with independently overridable TEST,
// TEST MATRIX PRIMARY, RED, GREEN and REFACTOR fields (or their outright
// omission when a field string is ""), so each RED case below can violate
// exactly one part of the TDD contract.
type tddFixture struct {
	id, phase        string
	done             bool
	test             string // "" omits the TEST field entirely
	primary          string // TEST MATRIX PRIMARY value; "" omits TEST MATRIX
	red, green, refc string // "" omits that field
	combinedRedGreen bool   // emit "RED/GREEN" instead of separate RED/GREEN
	evidence         string // "" omits Evidence; only meaningful when done
}

func (f tddFixture) render() string {
	var b strings.Builder
	check := " "
	if f.done {
		check = "x"
	}
	b.WriteString("- [" + check + "] `" + f.id + "` **[" + f.phase + "][LUNA] TDD fixture.**\n")
	b.WriteString("  - **Depends:** none.\n")
	b.WriteString("  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n")
	if f.test != "" {
		b.WriteString("  - **TEST:** `" + f.test + "`.\n")
	}
	if f.red != "" {
		b.WriteString("  - **RED:** " + f.red + ".\n")
	}
	if f.green != "" {
		b.WriteString("  - **GREEN:** " + f.green + ".\n")
	}
	if f.combinedRedGreen {
		// A spurious *additional* combined field: real malformed todos that
		// use ONLY "RED/GREEN" (no separate RED/GREEN) never reach ValidateTDD
		// as a Record at all, since todoregistry.ParseTodos rejects them for
		// missing RED/GREEN and reports a structural parse error instead
		// (see StructuralParseErrorSurfaces below); this exercises the
		// HasCombinedRedGreen detector on a block that otherwise parses.
		b.WriteString("  - **RED/GREEN:** combined field.\n")
	}
	if f.primary != "" {
		b.WriteString("  - **TEST MATRIX:** `PRIMARY=" + f.primary + "`.\n")
	}
	if f.refc != "" {
		b.WriteString("  - **REFACTOR:** " + f.refc + ".\n")
	}
	b.WriteString("  - **Refs:** [fixture](fixture.md).\n")
	if f.evidence != "" {
		b.WriteString("  - **Evidence (2026-09-05):** " + f.evidence + ".\n")
	}
	return b.String()
}

// wellFormed returns a fixture that satisfies every GOV-017 rule so callers
// can override exactly the one field their sub-test is exercising.
func wellFormed(id string) tddFixture {
	return tddFixture{
		id: id, phase: "P0",
		test: "Test" + strings.ReplaceAll(id, "-", ""), primary: "Test" + strings.ReplaceAll(id, "-", ""),
		red: "fixture red", green: "fixture green", refc: "fixture refactor",
	}
}

func parseOne(t *testing.T, f tddFixture) ([]Record, []error) {
	t.Helper()
	return ParseRecords(f.render())
}

// TestTodoTDDContractCompleteness is the PRIMARY test declared by `GOV-017`
// in planning/todos.md. It proves ValidateTDD rejects a missing TEST MATRIX
// PRIMARY mismatch, a combined RED/GREEN field, an invalid TEST identifier,
// a duplicate TEST name, and a checked-off todo whose Evidence field is
// missing, doesn't name its TEST, or doesn't report a go test result - then
// runs the same validator against the real registry and markdown and
// requires every finding to already be in the reviewed gov017Allowlist.
func TestTodoTDDContractCompleteness(t *testing.T) {
	t.Run("WellFormedIsSilent", func(t *testing.T) {
		records, errs := parseOne(t, wellFormed("FX-100"))
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		if len(findings) != 0 {
			t.Errorf("expected no findings for a well-formed unchecked todo, got: %v", findings)
		}
	})

	t.Run("PrimaryMismatch", func(t *testing.T) {
		f := wellFormed("FX-101")
		f.primary = "TestSomethingElse"
		records, errs := parseOne(t, f)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "FX-101", "GOV-017", CodePrimaryTestMismatch)
	})

	t.Run("CombinedRedGreen", func(t *testing.T) {
		f := wellFormed("FX-102")
		f.combinedRedGreen = true
		records, errs := parseOne(t, f)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "FX-102", "GOV-017", CodeCombinedRedGreen)
	})

	t.Run("CombinedRedGreenOnlyIsRejectedAtParse", func(t *testing.T) {
		// A todo that uses ONLY a "RED/GREEN" field (no separate RED/GREEN)
		// is rejected by todoregistry.ParseTodos itself - it never becomes a
		// Record - and surfaces as a GOV-017 structural parse error instead.
		broken := "- [ ] `FX-111` **[P0][LUNA] Combined-only fixture.**\n" +
			"  - **Depends:** none.\n" +
			"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n" +
			"  - **TEST:** `TestFX111`.\n" +
			"  - **TEST MATRIX:** `PRIMARY=TestFX111`.\n" +
			"  - **RED/GREEN:** combined field.\n" +
			"  - **REFACTOR:** rf.\n" +
			"  - **Refs:** [f](f.md).\n"
		records, errs := ParseRecords(broken)
		if len(errs) == 0 {
			t.Fatalf("expected todoregistry to reject the combined-only RED/GREEN field")
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "", "GOV-017", CodeStructuralParseError)
	})

	t.Run("InvalidTestName", func(t *testing.T) {
		f := wellFormed("FX-103")
		f.test = "checkSomething"
		f.primary = "checkSomething"
		records, errs := parseOne(t, f)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "FX-103", "GOV-017", CodeInvalidTestName)
	})

	t.Run("DuplicateTestName", func(t *testing.T) {
		a := wellFormed("FX-104")
		b := wellFormed("FX-105")
		b.test = a.test
		b.primary = a.test
		records, errs := ParseRecords(a.render() + b.render())
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "FX-104", "GOV-017", CodeDuplicateTestName)
		requireFinding(t, findings, "FX-105", "GOV-017", CodeDuplicateTestName)
	})

	t.Run("CheckedOffMissingEvidence", func(t *testing.T) {
		f := wellFormed("FX-106")
		f.done = true
		records, errs := parseOne(t, f)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "FX-106", "GOV-017", CodeMissingEvidence)
	})

	t.Run("CheckedOffEvidenceMissingTestName", func(t *testing.T) {
		f := wellFormed("FX-107")
		f.done = true
		f.evidence = "PASS; `go test` run on windows/arm64"
		records, errs := parseOne(t, f)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "FX-107", "GOV-017", CodeEvidenceMissingTestName)
	})

	t.Run("CheckedOffEvidenceMissingGoTestResult", func(t *testing.T) {
		f := wellFormed("FX-108")
		f.done = true
		f.evidence = "`" + f.test + "` PASSED"
		records, errs := parseOne(t, f)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "FX-108", "GOV-017", CodeEvidenceMissingGoTest)
	})

	t.Run("CheckedOffCompleteEvidenceIsSilent", func(t *testing.T) {
		f := wellFormed("FX-109")
		f.done = true
		f.evidence = "`" + f.test + "` PASS; `go test -count=1 ./fixture/` PASS"
		records, errs := parseOne(t, f)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateTDD(records, errs)
		if len(findings) != 0 {
			t.Errorf("expected no findings for complete evidence, got: %v", findings)
		}
	})

	t.Run("StructuralParseErrorSurfaces", func(t *testing.T) {
		// A block missing its TEST field is dropped by todoregistry.ParseTodos
		// entirely; GOV-017 must still surface that as a violation rather
		// than silently ignoring the todo.
		broken := "- [ ] `FX-110` **[P0][LUNA] Missing TEST.**\n" +
			"  - **Depends:** none.\n" +
			"  - **INTENT CONTEXT:** `ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n" +
			"  - **TEST MATRIX:** `PRIMARY=TestFX110`.\n" +
			"  - **RED:** r.\n  - **GREEN:** g.\n  - **REFACTOR:** rf.\n" +
			"  - **Refs:** [f](f.md).\n"
		records, errs := ParseRecords(broken)
		if len(errs) == 0 {
			t.Fatalf("expected todoregistry to reject the missing TEST field")
		}
		findings := ValidateTDD(records, errs)
		requireFinding(t, findings, "", "GOV-017", CodeStructuralParseError)
	})

	t.Run("RealCorpus", func(t *testing.T) {
		records := loadRealMarkdown(t)
		findings := ValidateTDD(records, nil)
		assertOnlyAllowlisted(t, findings, gov017Allowlist)
	})
}

// TestTodo_GOV_017_Golden pins the exact finding set for one small,
// hand-checked fixture set so a change to detection logic or message
// wording is caught even when it doesn't flip a pass/fail boundary.
func TestTodo_GOV_017_Golden(t *testing.T) {
	mismatched := wellFormed("G-101")
	mismatched.primary = "TestWrong"

	unchecked := wellFormed("G-102")

	checkedNoEvidence := wellFormed("G-103")
	checkedNoEvidence.done = true

	content := mismatched.render() + unchecked.render() + checkedNoEvidence.render()
	records, errs := ParseRecords(content)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	findings := ValidateTDD(records, errs)

	want := []string{
		"GOV-017|G-101|PRIMARY_TEST_MISMATCH|TestWrong",
		"GOV-017|G-103|MISSING_EVIDENCE|",
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
