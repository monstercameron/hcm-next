package todogovernance

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	CodeStructuralParseError    = "STRUCTURAL_PARSE_ERROR"
	CodeMissingTest             = "MISSING_TEST"
	CodeMissingTestMatrix       = "MISSING_TEST_MATRIX"
	CodePrimaryTestMismatch     = "PRIMARY_TEST_MISMATCH"
	CodeMissingRed              = "MISSING_RED"
	CodeMissingGreen            = "MISSING_GREEN"
	CodeMissingRefactor         = "MISSING_REFACTOR"
	CodeCombinedRedGreen        = "COMBINED_RED_GREEN"
	CodeInvalidTestName         = "INVALID_TEST_NAME"
	CodeDuplicateTestName       = "DUPLICATE_TEST_NAME"
	CodeMissingEvidence         = "MISSING_EVIDENCE"
	CodeEvidenceMissingTestName = "EVIDENCE_MISSING_TEST_NAME"
	CodeEvidenceMissingGoTest   = "EVIDENCE_MISSING_GO_TEST_RESULT"
)

var testNameRe = regexp.MustCompile(`^(Test|Fuzz|Benchmark)[A-Za-z0-9_]*$`)

// ValidateTDD runs the GOV-017 checks: every todo must declare TEST, a
// TEST MATRIX whose PRIMARY entry equals TEST, RED, GREEN and REFACTOR; a
// combined RED/GREEN field is prohibited; the TEST name must be a valid
// Go Test/Fuzz/Benchmark identifier and unique across the backlog; and a
// checked-off ("- [x]") todo must carry at least one Evidence line that
// names its TEST and reports a `go test` result.
//
// parseErrs are the structural errors returned by todoregistry.ParseTodos
// for blocks that were malformed enough to be dropped from the parsed todo
// list entirely (e.g. a missing TEST or TEST MATRIX field, or a duplicate
// ID) - those are themselves GOV-017 violations, since a todo that never
// resolves to a valid TodoContract cannot have proven its TDD contract.
func ValidateTDD(records []Record, parseErrs []error) []Finding {
	var findings []Finding

	for _, err := range parseErrs {
		findings = append(findings, Finding{
			Rule:   "GOV-017",
			Code:   CodeStructuralParseError,
			Reason: err.Error(),
		})
	}

	testOwners := map[string][]string{}

	for _, rec := range records {
		t := rec.Todo

		if t.Test == "" {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-017", Code: CodeMissingTest, Line: t.Line,
				Reason: "todo has no TEST field",
			})
		} else {
			if !testNameRe.MatchString(t.Test) {
				findings = append(findings, Finding{
					TodoID: t.ID, Rule: "GOV-017", Code: CodeInvalidTestName, Line: t.Line, Detail: t.Test,
					Reason: fmt.Sprintf("TEST %q is not a valid Test/Fuzz/Benchmark identifier", t.Test),
				})
			}
			testOwners[t.Test] = append(testOwners[t.Test], t.ID)
		}

		if len(t.TestMatrix) == 0 {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-017", Code: CodeMissingTestMatrix, Line: t.Line,
				Reason: "todo has no TEST MATRIX field",
			})
		} else if primary, ok := t.TestMatrix["PRIMARY"]; !ok || primary != t.Test {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-017", Code: CodePrimaryTestMismatch, Line: t.Line,
				Detail: primary,
				Reason: fmt.Sprintf("TEST MATRIX PRIMARY (%q) does not equal TEST (%q)", primary, t.Test),
			})
		}

		if t.Red == "" {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-017", Code: CodeMissingRed, Line: t.Line,
				Reason: "todo has no RED field",
			})
		}
		if t.Green == "" {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-017", Code: CodeMissingGreen, Line: t.Line,
				Reason: "todo has no GREEN field",
			})
		}
		if t.Refactor == "" {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-017", Code: CodeMissingRefactor, Line: t.Line,
				Reason: "todo has no REFACTOR field",
			})
		}
		if rec.HasCombinedRedGreen {
			findings = append(findings, Finding{
				TodoID: t.ID, Rule: "GOV-017", Code: CodeCombinedRedGreen, Line: t.Line,
				Reason: "todo declares a combined RED/GREEN field instead of separate RED and GREEN fields",
			})
		}

		if t.Done {
			findings = append(findings, validateEvidence(rec)...)
		}
	}

	testNames := make([]string, 0, len(testOwners))
	for name := range testOwners {
		testNames = append(testNames, name)
	}
	sort.Strings(testNames)
	for _, name := range testNames {
		owners := testOwners[name]
		if len(owners) < 2 {
			continue
		}
		sort.Strings(owners)
		for _, id := range owners {
			findings = append(findings, Finding{
				TodoID: id, Rule: "GOV-017", Code: CodeDuplicateTestName, Detail: name,
				Reason: fmt.Sprintf("TEST name %q is also declared by %s", name, strings.Join(without(owners, id), ", ")),
			})
		}
	}

	sortFindings(findings)
	return findings
}

func validateEvidence(rec Record) []Finding {
	t := rec.Todo
	if len(rec.EvidenceLines) == 0 {
		return []Finding{{
			TodoID: t.ID, Rule: "GOV-017", Code: CodeMissingEvidence, Line: t.Line,
			Reason: "todo is checked off but has no Evidence field",
		}}
	}

	joined := strings.Join(rec.EvidenceLines, "\n")
	var findings []Finding
	if t.Test != "" && !strings.Contains(joined, t.Test) {
		findings = append(findings, Finding{
			TodoID: t.ID, Rule: "GOV-017", Code: CodeEvidenceMissingTestName, Line: t.Line,
			Reason: fmt.Sprintf("Evidence field does not name TEST %q", t.Test),
		})
	}
	if !strings.Contains(joined, "go test") {
		findings = append(findings, Finding{
			TodoID: t.ID, Rule: "GOV-017", Code: CodeEvidenceMissingGoTest, Line: t.Line,
			Reason: "Evidence field does not report a `go test` result",
		})
	}
	return findings
}

func without(items []string, exclude string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it != exclude {
			out = append(out, it)
		}
	}
	return out
}
