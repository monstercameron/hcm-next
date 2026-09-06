package docintegrity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFindingAndReportFormatting_FilterAllowlist(t *testing.T) {
	finding := Finding{Code: CodeBrokenLink, Path: "planning/spec.md", Line: 4, Target: "missing.md", Detail: "not found"}
	if got := finding.Key(); got != "BROKEN_LINK|planning/spec.md|missing.md|not found" {
		t.Fatalf("Key()=%q", got)
	}
	if got := finding.String(); got != "planning/spec.md:4: BROKEN_LINK: not found (missing.md)" {
		t.Fatalf("String() with target=%q", got)
	}
	finding.Line = 0
	finding.Target = ""
	if got := finding.String(); got != "planning/spec.md: BROKEN_LINK: not found" {
		t.Fatalf("String() without target=%q", got)
	}
	report := Report{Findings: []Finding{{Code: "allowed", Allowlisted: true}, {Code: "violation"}}}
	violations := report.Violations()
	if len(violations) != 1 || violations[0].Code != "violation" {
		t.Fatalf("Violations()=%v", violations)
	}
}

func TestLoadTodoIDs_ValidatesInputAndDropsEmptyIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`[{"id":"A-1"},{"id":""},{"id":"A-1"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	ids, err := LoadTodoIDs(path)
	if err != nil || !reflect.DeepEqual(ids, map[string]bool{"A-1": true}) {
		t.Fatalf("LoadTodoIDs=%v err=%v", ids, err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "missing", path: filepath.Join(t.TempDir(), "missing.json"), want: "read todo registry"},
		{name: "malformed", path: bad, want: "parse todo registry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LoadTodoIDs(tc.path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadTodoIDs error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestEvaluateRepository_LoadsRegistryAndPropagatesErrors(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "planning/plan.md", "# Plan\n\n[spec](specs/spec.md#contract)\n")
	writeDoc(t, root, "planning/specs/spec.md", "# Spec\n\n## Contract\n\n`FX-001`\n")
	registry := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(registry, []byte(`[ {"id":"FX-001"} ]`), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateRepository(root, registry)
	if err != nil || len(report.Violations()) != 0 || len(report.NormativeDocuments) != 1 || report.TodoReferences[0] != "FX-001" {
		t.Fatalf("EvaluateRepository report=%+v err=%v", report, err)
	}
	if _, err := EvaluateRepository(root, filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("EvaluateRepository succeeded with missing registry")
	}
	if _, err := Evaluate(t.TempDir(), nil, nil); err == nil || !strings.Contains(err.Error(), "walk planning Markdown") {
		t.Fatalf("Evaluate missing planning error=%v", err)
	}
}

func TestEvaluate_ReportsSecurityRelevantDocumentFindings(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "planning/plan.md", "# Plan\n\n[spec](specs/spec.md#missing)\n[external](https://example.test/doc)\n![image](asset.png)\n[file](asset.txt)\n[]()\n")
	writeDoc(t, root, "planning/specs/spec.md", "# Spec\n\n## Contract\n\n`REQ-001` and `UNKNOWN-1`\n[missing](missing.md)\n")
	writeDoc(t, root, "planning/specs/other.md", "# Other\n\n`REQ-001`\n")
	writeDoc(t, root, "planning/notes.md", "# Notes\n\n[missing](missing.md)\n")
	report, err := Evaluate(root, map[string]bool{"KNOWN-1": true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{CodeMissingAnchor, CodeMissingDocument, CodeBrokenLink, CodeUnknownTodo, CodeDuplicateNormativeID, CodeOrphanDocument} {
		if !hasCode(report.Findings, code) {
			t.Errorf("findings=%v, missing %s", report.Findings, code)
		}
	}
	if report.OrphanCount != 1 || len(report.NormativeDocuments) != 2 || len(report.TodoReferences) != 2 {
		t.Fatalf("report counts=%+v, want orphan 1, two normative docs and two todo refs", report)
	}
	if hasTarget(report.Findings, "https://example.test/doc") || hasTarget(report.Findings, "asset.png") || hasTarget(report.Findings, "asset.txt") {
		t.Fatalf("external/image/non-Markdown links were incorrectly diagnosed: %v", report.Findings)
	}
}

func TestMarkdownParsingHelpers_HandleLinksAnchorsAndTargets(t *testing.T) {
	links := markdownLinks("[one](a.md) ![image](image.png) [two](<b%20file.md#Heading?ignored>)")
	if len(links) != 3 || links[0].Target != "a.md" || !strings.Contains(links[2].Target, "b%20file.md") {
		t.Fatalf("markdownLinks=%+v", links)
	}
	target, fragment := splitTarget("<b%20file.md?query=1#Some Heading>")
	if target != "b file.md" || fragment != "some-heading" {
		t.Fatalf("splitTarget=%q,%q", target, fragment)
	}
	for _, tc := range []struct {
		target   string
		external bool
	}{
		{target: "", external: false},
		{target: "#anchor", external: false},
		{target: "https://example.test", external: true},
		{target: "mailto:test@example.test", external: true},
		{target: "doc.md", external: false},
	} {
		if got := isExternalTarget(tc.target); got != tc.external {
			t.Errorf("isExternalTarget(%q)=%v, want %v", tc.target, got, tc.external)
		}
	}
	anchors := headingAnchors("# Title\n# Title\n<a id=\"custom\"></a>\n<a name=\"named\"></a>\n## Long—Dash\n")
	for _, anchor := range []string{"title", "title-1", "custom", "named", "long-dash"} {
		if !anchors[anchor] {
			t.Errorf("heading anchors=%v, missing %q", anchors, anchor)
		}
	}
	if got := normalizeAnchor("  Repeated   spaces — here! "); got != "repeated-spaces-here" {
		t.Fatalf("normalizeAnchor=%q", got)
	}
	if !isASCII("plain ASCII") || isASCII("caf\u00e9") {
		t.Fatal("isASCII classification incorrect")
	}
	if got := todoReferences("Z-2 A-1 A-1"); !reflect.DeepEqual(got, []string{"A-1", "Z-2"}) {
		t.Fatalf("todoReferences=%v", got)
	}
}

func TestResolveTargetAndSyntaxHelpers_CoverBoundaries(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "planning/specs/README.md", "# README\n")
	writeDoc(t, root, "planning/specs/index.md", "# Index\n")
	files := map[string]string{"planning/specs/spec.md": filepath.Join(root, "planning/specs/spec.md")}
	if _, rel, ok := resolveTarget(root, "planning/plan.md", "specs/spec.md", files); !ok || rel != "planning/specs/spec.md" {
		t.Fatalf("resolveTarget mapped file rel=%q ok=%v", rel, ok)
	}
	if _, rel, ok := resolveTarget(root, "planning/plan.md", "specs", map[string]string{}); !ok || rel != "planning/specs/README.md" {
		t.Fatalf("resolveTarget README rel=%q ok=%v", rel, ok)
	}
	if _, rel, ok := resolveTarget(root, "planning/plan.md", "specs/index.md", map[string]string{"planning/specs/index.md": filepath.Join(root, "planning/specs/index.md")}); !ok || rel != "planning/specs/index.md" {
		t.Fatalf("resolveTarget index rel=%q ok=%v", rel, ok)
	}
	if _, _, ok := resolveTarget(root, "planning/plan.md", "missing.md", files); ok {
		t.Fatal("resolveTarget reported missing file as present")
	}
	if got := lineNumber("one\ntwo\n", 4); got != 2 || lineNumber("one", -1) != 0 {
		t.Fatalf("lineNumber boundary results=%d,%d", got, lineNumber("one", -1))
	}
	if got := slashPath(filepath.Join("planning", "..", "specs", "a.md")); got != "specs/a.md" {
		t.Fatalf("slashPath=%q", got)
	}
	if got := unique([]string{"a", "a", "b", "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("unique=%v", got)
	}
	if got := unique([]string{"one"}); !reflect.DeepEqual(got, []string{"one"}) || unique(nil) != nil {
		t.Fatalf("unique short inputs incorrect: %v", got)
	}

	cases := []struct {
		name string
		data []byte
		code string
	}{
		{name: "invalid utf8", data: []byte{0xff}, code: CodeInvalidUTF8},
		{name: "unbalanced backtick", data: []byte("```go\nopen\n"), code: CodeUnbalancedFence},
		{name: "unbalanced tilde", data: []byte("~~~go\nopen\n"), code: CodeUnbalancedFence},
		{name: "ascii fence", data: []byte("```TEXT-ASCII\nplain\n```\n"), code: ""},
		{name: "non ascii ascii fence", data: []byte("```text-ascii\ncaf\u00e9\n```\n"), code: CodeInvalidASCIIBlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := documentSyntaxFindings("fixture.md", tc.data)
			if tc.code == "" {
				if len(findings) != 0 {
					t.Fatalf("findings=%v, want clean syntax", findings)
				}
				return
			}
			if !hasCode(findings, tc.code) {
				t.Fatalf("findings=%v, missing %s", findings, tc.code)
			}
		})
	}
	if _, err := markdownFiles(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("markdownFiles succeeded for missing directory")
	}
	if _, err := json.Marshal(Report{}); err != nil {
		t.Fatal(err)
	}
}

func hasTarget(findings []Finding, target string) bool {
	for _, finding := range findings {
		if finding.Target == target {
			return true
		}
	}
	return false
}
