package docintegrity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoc(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func docFixture(t *testing.T, plan, spec string) (string, map[string]bool) {
	t.Helper()
	root := t.TempDir()
	writeDoc(t, root, "planning/plan.md", plan)
	writeDoc(t, root, "planning/specs/spec.md", spec)
	return root, map[string]bool{"FX-001": true}
}

func findingCodes(findings []Finding) []string {
	codes := make([]string, len(findings))
	for i, finding := range findings {
		codes[i] = finding.Code
	}
	return codes
}

// TestTodo_DOC_001 is the PRIMARY test declared by DOC-001. It proves clean
// resolution and stable diagnostics for a missing document, anchor and todo,
// as well as owner-backed orphan allowlisting.
func TestTodo_DOC_001(t *testing.T) {
	t.Run("CleanCorpus", func(t *testing.T) {
		root, ids := docFixture(t, "# Plan\n\n[spec](specs/spec.md#contract)\n", "# Spec\n\n## Contract\n\nThe `FX-001` contract.\n")
		report, err := Evaluate(root, ids, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := report.Violations(); len(got) != 0 {
			t.Fatalf("clean fixture violations = %v", got)
		}
	})

	t.Run("BrokenReferencesAndOwnedOrphan", func(t *testing.T) {
		root, ids := docFixture(t, "# Plan\n\n[missing](missing.md#gone)\n", "# Spec\n\nThe `UNKNOWN-999` contract.\n")
		writeDoc(t, root, "planning/notes.md", "# Historical notes\n")
		report, err := Evaluate(root, ids, map[string]string{"planning/notes.md": "planning-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if report.OrphanCount != 1 || report.AllowlistedOrphans != 1 {
			t.Fatalf("orphan counts = %d/%d, want 1/1", report.OrphanCount, report.AllowlistedOrphans)
		}
		if len(report.Violations()) != 2 {
			t.Fatalf("violations = %v, want missing document, unknown todo and no orphan violation", report.Violations())
		}
		for _, code := range []string{CodeMissingDocument, CodeUnknownTodo} {
			found := false
			for _, finding := range report.Violations() {
				if finding.Code == code {
					found = true
				}
			}
			if !found {
				t.Errorf("missing %s in %v", code, report.Violations())
			}
		}
	})

	t.Run("AnchorsAreChecked", func(t *testing.T) {
		root, ids := docFixture(t, "# Plan\n\n[spec](specs/spec.md#missing-anchor)\n", "# Spec\n\n## Contract\n\nThe `FX-001` contract.\n")
		report, err := Evaluate(root, ids, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !hasCode(report.Findings, CodeMissingAnchor) {
			t.Fatalf("findings = %v, want %s", report.Findings, CodeMissingAnchor)
		}
	})

	t.Run("SyntaxIsChecked", func(t *testing.T) {
		if !hasCode(documentSyntaxFindings("bad.md", []byte{0xff}), CodeInvalidUTF8) {
			t.Error("invalid UTF-8 was not rejected")
		}
		if !hasCode(documentSyntaxFindings("fence.md", []byte("# x\n```go\nopen\n")), CodeUnbalancedFence) {
			t.Error("unbalanced fence was not rejected")
		}
		if !hasCode(documentSyntaxFindings("ascii.md", []byte("```text-ascii\nnot ok: café\n```\n")), CodeInvalidASCIIBlock) {
			t.Error("non-ASCII text-ascii block was not rejected")
		}
	})

	t.Run("DuplicateNormativeID", func(t *testing.T) {
		root, ids := docFixture(t, "# Plan\n", "# Spec\n\n`REQ-001`\n")
		writeDoc(t, root, "planning/specs/other.md", "# Other\n\n`REQ-001`\n")
		report, err := Evaluate(root, ids, nil)
		if err != nil {
			t.Fatal(err)
		}
		if countCode(report.Findings, CodeDuplicateNormativeID) != 2 {
			t.Fatalf("duplicate findings = %v, want two locations", report.Findings)
		}
	})
}

func TestTodo_DOC_001_Property(t *testing.T) {
	for _, heading := range []string{"Simple heading", "Heading with punctuation!", "Heading with café"} {
		anchor := normalizeAnchor(heading)
		if !headingAnchors("# " + heading + "\n")[anchor] {
			t.Errorf("heading %q did not produce anchor %q", heading, anchor)
		}
	}
	if got := normalizeAnchor("Repeated   spaces"); got != "repeated-spaces" {
		t.Errorf("normalizeAnchor = %q", got)
	}
}

func TestTodo_DOC_001_Golden(t *testing.T) {
	root, ids := docFixture(t, "# Plan\n\n[missing](missing.md#gone)\n", "# Spec\n\nThe `UNKNOWN-999` contract.\n")
	report, err := Evaluate(root, ids, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{CodeMissingDocument, CodeUnknownTodo}
	if got := findingCodes(report.Findings); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("finding codes = %v, want %v", got, want)
	}
}

func TestTodo_DOC_001_Integration(t *testing.T) {
	root, ids := docFixture(t, "# Plan\n\n[spec](specs/spec.md#contract)\n", "# Spec\n\n## Contract\n\n`FX-001`\n")
	if _, err := Evaluate(root, ids, nil); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
}

func FuzzTodo_DOC_001(f *testing.F) {
	for _, seed := range []string{"", "# Heading", "## Punctuation / café", "```text-ascii\nplain\n```"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		_ = headingAnchors(content)
		_ = documentSyntaxFindings("fixture.md", []byte(content))
		_ = markdownLinks(content)
	})
}

func hasCode(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func countCode(findings []Finding, code string) int {
	count := 0
	for _, finding := range findings {
		if finding.Code == code {
			count++
		}
	}
	return count
}
