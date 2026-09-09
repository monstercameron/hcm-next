package report

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/discover"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/internal/reporoot"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/runner"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

func testSetup(t *testing.T) (repoRoot, refRoot, runtimeSpecPath, contextSpecPath string, v *vocab.Vocabulary) {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	runtimeSpecPath = filepath.Join(root, "planning", "specs", "workflow-runtime.md")
	contextSpecPath = filepath.Join(root, "planning", "workflows", "_engine", "workflow-context-contract.md")
	refRoot = filepath.Join(root, "planning", "reference-workflows")
	v, err = vocab.Load(runtimeSpecPath)
	if err != nil {
		t.Fatalf("vocab.Load: %v", err)
	}
	return root, refRoot, runtimeSpecPath, contextSpecPath, v
}

func TestBuildOverRealReferenceWorkflows(t *testing.T) {
	repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v := testSetup(t)

	results, err := discover.Documents(repoRoot, refRoot, v)
	if err != nil {
		t.Fatalf("discover.Documents: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3 (manager-change.md, promote-into-management.md, reference-suite.md)", len(results))
	}

	rpt := Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v, results, runner.DefaultFixedClock)

	if rpt.ExecutionMode != "SIMULATE" {
		t.Errorf("ExecutionMode = %q, want SIMULATE", rpt.ExecutionMode)
	}
	if rpt.RootPath != "planning/reference-workflows" {
		t.Errorf("RootPath = %q, want planning/reference-workflows", rpt.RootPath)
	}
	if len(rpt.ManifestDigest) != 64 {
		t.Errorf("ManifestDigest = %q, want a 64-character sha256 hex digest", rpt.ManifestDigest)
	}
	if len(rpt.Documents) != 3 {
		t.Fatalf("len(Documents) = %d, want 3", len(rpt.Documents))
	}
	for i := 1; i < len(rpt.Documents); i++ {
		if rpt.Documents[i-1].SourcePath >= rpt.Documents[i].SourcePath {
			t.Fatalf("Documents not sorted: %s before %s", rpt.Documents[i-1].SourcePath, rpt.Documents[i].SourcePath)
		}
	}

	totalWorkflows := 0
	for _, doc := range rpt.Documents {
		for i := 1; i < len(doc.Workflows); i++ {
			if doc.Workflows[i-1].ID >= doc.Workflows[i].ID {
				t.Errorf("%s workflows not sorted: %s before %s", doc.SourcePath, doc.Workflows[i-1].ID, doc.Workflows[i].ID)
			}
		}
		for _, wf := range doc.Workflows {
			totalWorkflows++
			for i := 1; i < len(wf.Checks); i++ {
				if wf.Checks[i-1].ID >= wf.Checks[i].ID {
					t.Errorf("%s checks not sorted: %s before %s", wf.ID, wf.Checks[i-1].ID, wf.Checks[i].ID)
				}
			}
			gotTotal := wf.Summary.Pass + wf.Summary.Fail + wf.Summary.Unknown
			if gotTotal != len(wf.Checks) {
				t.Errorf("%s summary totals %d, want %d (len(Checks))", wf.ID, gotTotal, len(wf.Checks))
			}
		}
	}
	// 2 standalone documents + 6 suite-section workflows.
	if totalWorkflows != 8 {
		t.Errorf("totalWorkflows = %d, want 8", totalWorkflows)
	}

	// JSON must round-trip.
	data, err := rpt.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var roundTrip Report
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("Unmarshal(JSON output): %v", err)
	}
	if roundTrip.ManifestDigest != rpt.ManifestDigest {
		t.Errorf("round-tripped digest %q != original %q", roundTrip.ManifestDigest, rpt.ManifestDigest)
	}

	md := rpt.Markdown()
	if len(md) == 0 {
		t.Error("Markdown() is empty")
	}
}

func TestBuildIsDeterministicAcrossRuns(t *testing.T) {
	repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v := testSetup(t)

	results1, err := discover.Documents(repoRoot, refRoot, v)
	if err != nil {
		t.Fatalf("discover.Documents (1): %v", err)
	}
	results2, err := discover.Documents(repoRoot, refRoot, v)
	if err != nil {
		t.Fatalf("discover.Documents (2): %v", err)
	}

	rpt1 := Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v, results1, runner.DefaultFixedClock)
	rpt2 := Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v, results2, runner.DefaultFixedClock)

	data1, err := rpt1.JSON()
	if err != nil {
		t.Fatalf("JSON (1): %v", err)
	}
	data2, err := rpt2.JSON()
	if err != nil {
		t.Fatalf("JSON (2): %v", err)
	}
	if string(data1) != string(data2) {
		t.Fatal("two Build() runs over the same input produced different JSON output")
	}

	md1 := rpt1.Markdown()
	md2 := rpt2.Markdown()
	if string(md1) != string(md2) {
		t.Fatal("two Build() runs over the same input produced different Markdown output")
	}
}

func TestParseErrorDocumentIsIsolated(t *testing.T) {
	repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v := testSetup(t)
	results, err := discover.Documents(repoRoot, refRoot, v)
	if err != nil {
		t.Fatalf("discover.Documents: %v", err)
	}

	// Inject a synthetic parse failure alongside the real, valid results
	// to prove Build reports it as an isolated PARSE_ERROR workflow rather
	// than dropping it or aborting the whole batch.
	results = append(results, discover.FileResult{RelPath: "planning/reference-workflows/broken.md", Err: errUnreadable})

	rpt := Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath, v, results, runner.DefaultFixedClock)

	var found *DocumentReport
	for i := range rpt.Documents {
		if rpt.Documents[i].SourcePath == "planning/reference-workflows/broken.md" {
			found = &rpt.Documents[i]
		}
	}
	if found == nil {
		t.Fatal("broken.md not found in report; a parse failure must still produce a document entry")
	}
	if len(found.Workflows) != 1 || found.Workflows[0].Kind != "PARSE_ERROR" {
		t.Errorf("broken.md workflows = %+v, want one PARSE_ERROR workflow", found.Workflows)
	}
}

type stubErr string

func (e stubErr) Error() string { return string(e) }

const errUnreadable = stubErr("stub: unreadable fixture")
