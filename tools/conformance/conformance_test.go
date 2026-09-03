package main

// CONF-001 test matrix.
//
// The 2026-09-02 disposition on CONF-001 in planning/todos.md scopes P1A to
// "the depth the no-effect Promotion fixture needs (simulate mode, fixed
// clock, zero-effect receipt)" and explicitly defers replay and fault
// schedules to P1B. No compiled workflow engine exists yet to execute a
// reference workflow or to inject a runtime fault into it (this package
// works on the reference-workflow documents as data; see runner.Runner).
// So the Fault and Recovery tests below exercise the failure modes that
// actually exist at this depth -- a malformed or unreadable input
// document -- rather than simulating an execution-engine crash or a
// network fault that nothing in this lane can produce yet. This is
// recorded as a scope decision in the CONF-001 report handed back with
// this lane's work, not silently substituted.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/conformance/checks"
	"github.com/monstercameron/hcm-next/tools/conformance/discover"
	"github.com/monstercameron/hcm-next/tools/conformance/internal/reporoot"
	"github.com/monstercameron/hcm-next/tools/conformance/model"
	"github.com/monstercameron/hcm-next/tools/conformance/parse"
	"github.com/monstercameron/hcm-next/tools/conformance/report"
	"github.com/monstercameron/hcm-next/tools/conformance/runner"
	"github.com/monstercameron/hcm-next/tools/conformance/vocab"
)

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	return root
}

func mustVocab(t *testing.T, repoRoot string) *vocab.Vocabulary {
	t.Helper()
	v, err := vocab.Load(filepath.Join(repoRoot, "planning", "specs", "workflow-runtime.md"))
	if err != nil {
		t.Fatalf("vocab.Load: %v", err)
	}
	return v
}

// TestTodo_CONF_001 is CONF-001's primary test: `go run ./tools/conformance`
// over the real planning/reference-workflows tree parses every document,
// evaluates it in SIMULATE mode against a fixed clock with a zero-effect
// receipt, and writes a report carrying a manifest digest.
func TestTodo_CONF_001(t *testing.T) {
	outDir := t.TempDir()
	repoRoot := mustRepoRoot(t)
	refRoot := filepath.Join(repoRoot, "planning", "reference-workflows")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", refRoot, "-out", outDir, "-format", "both"}, &stdout, &stderr)
	if code != 0 && code != 1 {
		t.Fatalf("run() exit code = %d, want 0 or 1 (1 only if a real document FAILs a check); stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}

	jsonPath := filepath.Join(outDir, "report.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read %s: %v", jsonPath, err)
	}
	var rpt report.Report
	if err := json.Unmarshal(data, &rpt); err != nil {
		t.Fatalf("unmarshal report.json: %v", err)
	}

	if rpt.ExecutionMode != "SIMULATE" {
		t.Errorf("ExecutionMode = %q, want SIMULATE", rpt.ExecutionMode)
	}
	if rpt.FixedClock != runner.DefaultFixedClock.At.UTC().Format("2006-01-02T15:04:05Z07:00") {
		t.Errorf("FixedClock = %q, want the runner's DefaultFixedClock instant", rpt.FixedClock)
	}
	if len(rpt.ManifestDigest) != 64 {
		t.Errorf("ManifestDigest = %q, want a 64-character sha256 hex digest", rpt.ManifestDigest)
	}
	if len(rpt.Documents) != 3 {
		t.Fatalf("len(Documents) = %d, want 3", len(rpt.Documents))
	}

	total := 0
	for _, doc := range rpt.Documents {
		total += len(doc.Workflows)
		for _, wf := range doc.Workflows {
			if len(wf.Checks) == 0 {
				t.Errorf("%s/%s has no checks", doc.SourcePath, wf.ID)
			}
		}
	}
	if total != 8 {
		t.Errorf("total workflows = %d, want 8 (2 standalone + 6 suite sections)", total)
	}

	if _, err := os.Stat(filepath.Join(outDir, "report.md")); err != nil {
		t.Errorf("report.md missing: %v", err)
	}
}

// TestTodo_CONF_001_Golden pins the promotion reference workflow's report
// to a checked-in golden fixture. Regenerate the golden file deliberately
// (not by patching the assertion) whenever promote-into-management.md,
// workflow-runtime.md's vocabulary, or a check's logic changes on purpose.
func TestTodo_CONF_001_Golden(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	refRoot := filepath.Join(repoRoot, "planning", "reference-workflows")
	runtimeSpecPath := filepath.Join(repoRoot, "planning", "specs", "workflow-runtime.md")
	contextSpecPath := filepath.Join(repoRoot, "planning", "workflows", "_engine", "workflow-context-contract.md")
	v := mustVocab(t, repoRoot)

	results, err := discover.Documents(repoRoot, refRoot, v)
	if err != nil {
		t.Fatalf("discover.Documents: %v", err)
	}
	var filtered []discover.FileResult
	for _, r := range results {
		if filepath.Base(r.RelPath) == "promote-into-management.md" {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) != 1 {
		t.Fatalf("expected exactly one promote-into-management.md result, got %d", len(filtered))
	}

	rpt := report.Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v, filtered, runner.DefaultFixedClock)
	got, err := rpt.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	goldenPath := filepath.Join(repoRoot, "tools", "conformance", "testdata", "golden", "promote-into-management.report.json")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden fixture %s: %v (regenerate it if this is an intentional change)", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		gotPath := filepath.Join(t.TempDir(), "got.report.json")
		_ = os.WriteFile(gotPath, got, 0o644)
		t.Fatalf("report for promote-into-management.md does not match the golden fixture %s; actual output written to %s for diffing", goldenPath, gotPath)
	}
}

// TestTodo_CONF_001_Conformance proves the runner's own determinism
// requirement: running the report generator twice over the same real
// input tree produces byte-identical JSON, and every check result is
// internally well-formed (a valid status, unique sorted IDs, non-empty
// evidence).
func TestTodo_CONF_001_Conformance(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	refRoot := filepath.Join(repoRoot, "planning", "reference-workflows")
	runtimeSpecPath := filepath.Join(repoRoot, "planning", "specs", "workflow-runtime.md")
	contextSpecPath := filepath.Join(repoRoot, "planning", "workflows", "_engine", "workflow-context-contract.md")
	v := mustVocab(t, repoRoot)

	build := func() []byte {
		results, err := discover.Documents(repoRoot, refRoot, v)
		if err != nil {
			t.Fatalf("discover.Documents: %v", err)
		}
		rpt := report.Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v, results, runner.DefaultFixedClock)

		seen := map[string]bool{}
		for _, doc := range rpt.Documents {
			for _, wf := range doc.Workflows {
				lastID := ""
				for _, c := range wf.Checks {
					switch c.Status {
					case "PASS", "FAIL", "UNKNOWN":
					default:
						t.Errorf("%s/%s check %s has invalid status %q", doc.SourcePath, wf.ID, c.ID, c.Status)
					}
					if c.Detail == "" {
						t.Errorf("%s/%s check %s has empty Detail", doc.SourcePath, wf.ID, c.ID)
					}
					if c.ID <= lastID {
						t.Errorf("%s/%s checks not strictly sorted at %s", doc.SourcePath, wf.ID, c.ID)
					}
					lastID = c.ID
				}
				key := doc.SourcePath + "\x00" + wf.ID
				if seen[key] {
					t.Errorf("duplicate workflow id %s in %s", wf.ID, doc.SourcePath)
				}
				seen[key] = true
			}
		}

		data, err := rpt.JSON()
		if err != nil {
			t.Fatalf("JSON: %v", err)
		}
		return data
	}

	first := build()
	second := build()
	if !bytes.Equal(first, second) {
		t.Fatal("two Build() runs over the same real reference-workflow tree produced different JSON; report generation must be byte-identical across runs")
	}
}

// TestTodo_CONF_001_Fault proves that one malformed reference-workflow
// document (missing its required "Required conformance scenarios" section
// and its level-1 title) does not corrupt or drop the report for its
// well-formed siblings: discovery isolates the failure per file, and the
// resulting report carries a clearly diagnosable entry for it instead of a
// panic or a silently truncated batch.
func TestTodo_CONF_001_Fault(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	v := mustVocab(t, repoRoot)
	dir := t.TempDir()

	writeFixture(t, dir, "good.md", "# Good Workflow\n\n## Required conformance scenarios\n\n1. Happy path.\n")
	writeFixture(t, dir, "malformed.md", "Not a title at all.\n\nNo structure here.\n")

	results, err := discover.Documents(repoRoot, dir, v)
	if err != nil {
		t.Fatalf("discover.Documents: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	var goodResult, badResult *discover.FileResult
	for i := range results {
		switch filepath.Base(results[i].RelPath) {
		case "good.md":
			goodResult = &results[i]
		case "malformed.md":
			badResult = &results[i]
		}
	}
	if goodResult == nil || badResult == nil {
		t.Fatalf("did not find both fixtures in results: %+v", results)
	}
	if badResult.Err == nil {
		t.Fatal("malformed.md: Err = nil, want a parse error (no level-1 title)")
	}
	if goodResult.Err != nil {
		t.Fatalf("good.md: Err = %v, want nil (a sibling failure must not affect it)", goodResult.Err)
	}

	rpt := report.Build(repoRoot, dir, filepath.Join(repoRoot, "planning", "specs", "workflow-runtime.md"), filepath.Join(repoRoot, "planning", "workflows", "_engine", "workflow-context-contract.md"), v, results, runner.DefaultFixedClock)
	if len(rpt.Documents) != 2 {
		t.Fatalf("len(Documents) = %d, want 2", len(rpt.Documents))
	}
	for _, doc := range rpt.Documents {
		if filepath.Base(doc.SourcePath) == "malformed.md" {
			if len(doc.Workflows) != 1 || doc.Workflows[0].Kind != "PARSE_ERROR" {
				t.Errorf("malformed.md report = %+v, want one PARSE_ERROR workflow", doc.Workflows)
			}
		}
		if filepath.Base(doc.SourcePath) == "good.md" {
			if len(doc.Workflows) != 1 || doc.Workflows[0].Kind != model.KindDocument {
				t.Errorf("good.md report = %+v, want one full workflow report", doc.Workflows)
			}
			if len(doc.Workflows[0].Checks) == 0 {
				t.Error("good.md workflow has no checks despite parsing successfully")
			}
		}
	}
}

// TestTodo_CONF_001_Recovery proves the batch recovers from a permanently
// unreadable input (a directory that shares the *.md naming convention,
// which os.ReadFile cannot read as a file) without aborting discovery of
// its siblings, and that the CLI itself still exits with a diagnosable,
// non-panicking result for the whole run.
func TestTodo_CONF_001_Recovery(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	v := mustVocab(t, repoRoot)
	dir := t.TempDir()

	writeFixture(t, dir, "good.md", "# Good Workflow\n\n## Required conformance scenarios\n\n1. Happy path.\n")
	if err := os.Mkdir(filepath.Join(dir, "broken.md"), 0o755); err != nil {
		t.Fatalf("Mkdir broken.md: %v", err)
	}

	results, err := discover.Documents(repoRoot, dir, v)
	if err != nil {
		t.Fatalf("discover.Documents: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	var brokenErr error
	goodOK := false
	for _, r := range results {
		switch filepath.Base(r.RelPath) {
		case "broken.md":
			brokenErr = r.Err
		case "good.md":
			goodOK = r.Err == nil && r.Doc != nil
		}
	}
	if brokenErr == nil {
		t.Fatal("broken.md (a directory): Err = nil, want a read error")
	}
	if !goodOK {
		t.Fatal("good.md did not parse successfully despite the unrelated unreadable sibling")
	}

	// Drive it through the CLI entry point too: it must not panic and must
	// still produce a report directory.
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", dir, "-out", outDir, "-format", "json"}, &stdout, &stderr)
	if code == 2 {
		t.Fatalf("run() exit code = 2 (usage/setup error), stderr=%s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "report.json")); err != nil {
		t.Errorf("report.json missing after recovery run: %v", err)
	}
}

// TestTodo_CONF_001_Mutation applies targeted, isolated mutations to the
// real promote-into-management.md text and asserts that the specific
// check each mutation should break actually flips status. This guards
// against checks that always report PASS regardless of document content.
func TestTodo_CONF_001_Mutation(t *testing.T) {
	repoRoot := mustRepoRoot(t)
	v := mustVocab(t, repoRoot)
	original, err := os.ReadFile(filepath.Join(repoRoot, "planning", "reference-workflows", "promote-into-management.md"))
	if err != nil {
		t.Fatalf("read promote-into-management.md: %v", err)
	}
	baselineStatus := func(chkID string) checks.Status {
		doc, err := parse.Parse("promote-into-management.md", original, v)
		if err != nil {
			t.Fatalf("parse baseline: %v", err)
		}
		for _, r := range checks.Evaluate(doc.Workflows[0], v) {
			if r.ID == chkID {
				return r.Status
			}
		}
		t.Fatalf("check %s not found in baseline results", chkID)
		return ""
	}
	mutatedStatus := func(t *testing.T, mutated []byte, chkID string) checks.Status {
		t.Helper()
		doc, err := parse.Parse("promote-into-management.md", mutated, v)
		if err != nil {
			t.Fatalf("parse mutated: %v", err)
		}
		for _, r := range checks.Evaluate(doc.Workflows[0], v) {
			if r.ID == chkID {
				return r.Status
			}
		}
		t.Fatalf("check %s not found in mutated results", chkID)
		return ""
	}

	t.Run("CHK-002 flips when the P1B gating annotation is removed", func(t *testing.T) {
		const chkID = "CHK-002"
		if got := baselineStatus(chkID); got != checks.Pass {
			t.Fatalf("baseline %s = %s, want PASS", chkID, got)
		}
		mutated := bytes.ReplaceAll(original,
			[]byte("is the post-P1B shape once `PARALLEL`/`JOIN` exist."),
			[]byte("is the post shape once `PARALLEL`/`JOIN` exist."))
		if bytes.Equal(mutated, original) {
			t.Fatal("mutation did not change the document; the target sentence text has drifted")
		}
		if got := mutatedStatus(t, mutated, chkID); got != checks.Fail {
			t.Errorf("mutated %s = %s, want FAIL", chkID, got)
		}
	})

	t.Run("CHK-007 flips when the scenarios section is deleted", func(t *testing.T) {
		const chkID = "CHK-007"
		if got := baselineStatus(chkID); got != checks.Pass {
			t.Fatalf("baseline %s = %s, want PASS", chkID, got)
		}
		idx := bytes.Index(original, []byte("## Required conformance scenarios"))
		if idx == -1 {
			t.Fatal("could not find the scenarios heading to delete; the document has drifted")
		}
		mutated := append([]byte(nil), original[:idx]...)
		if got := mutatedStatus(t, mutated, chkID); got != checks.Fail {
			t.Errorf("mutated %s = %s, want FAIL", chkID, got)
		}
	})

	t.Run("CHK-006 flips when a completion dimension is renamed", func(t *testing.T) {
		const chkID = "CHK-006"
		if got := baselineStatus(chkID); got != checks.Pass {
			t.Fatalf("baseline %s = %s, want PASS", chkID, got)
		}
		mutated := bytes.Replace(original, []byte("ConsistencyState   DEGRADED"), []byte("ConsistencyStateX  DEGRADED"), 1)
		if bytes.Equal(mutated, original) {
			t.Fatal("mutation did not change the document; the target line text has drifted")
		}
		if got := mutatedStatus(t, mutated, chkID); got != checks.Fail {
			t.Errorf("mutated %s = %s, want FAIL", chkID, got)
		}
	})
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
