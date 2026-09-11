package scenariomatrix

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion"
)

func naDimension(reason string) workflowdesign.Dimension {
	return workflowdesign.Dimension{Value: workflowdesign.NotApplicable, Reason: reason}
}

func matrixRecord() workflowdesign.DesignRecord {
	return workflowdesign.DesignRecord{
		Intent:         "ChangeManager",
		Definition:     "hcmnext.people.change_manager/v1",
		Disposition:    workflowdesign.DispositionWorkflow,
		Archetype:      "A2",
		DomainProfile:  "PPL",
		InputBoundary:  "manager-edge-proposal",
		SnapshotPolicy: "authority-snapshot",
		Engines:        workflowdesign.Dimension{Items: []string{"identity-resolution"}},
		HumanWork:      workflowdesign.Dimension{Value: "manager-self-service"},
		Writes:         workflowdesign.Dimension{Value: "manager-edge-replacement"},
		Waits:          workflowdesign.Dimension{Value: "approval-window"},
		Invalidators:   workflowdesign.Dimension{Value: "org-reparent"},
		Reconciliation: workflowdesign.Dimension{Value: "relationship-access"},
		Correction:     workflowdesign.Dimension{Value: "revoke-and-replace"},
		Completion:     "edge-observed",
	}
}

func matrixRecipe() workflowexpansion.Recipe {
	return workflowexpansion.Recipe{Archetype: "A2", Phases: []string{
		"resolve current fact and expected version",
		"approve when policy requires",
		"atomic end and append revision with evidence",
		"observe and reconcile effects",
		"close the revision",
	}}
}

func matrixProfile() workflowexpansion.Profile {
	return workflowexpansion.Profile{Code: "PPL", Reads: "person facts", Writes: "people facts", Closure: "reconciliation"}
}

func matrixGraph(t *testing.T, record workflowdesign.DesignRecord) *workflowexpansion.Graph {
	t.Helper()
	graph, findings := workflowexpansion.Expand(workflowexpansion.Input{Recipe: matrixRecipe(), Profile: matrixProfile(), Record: record})
	if len(findings) > 0 {
		t.Fatalf("matrix fixture failed expansion: %+v", findings)
	}
	return graph
}

func generateFixture(t *testing.T) (*Matrix, []Finding) {
	t.Helper()
	record := matrixRecord()
	return Generate(record, matrixGraph(t, record), matrixProfile())
}

func scenarioNames(matrix *Matrix) map[string]Scenario {
	out := make(map[string]Scenario)
	for _, scenario := range matrix.Scenarios {
		out[scenario.Name] = scenario
	}
	return out
}

// TestWorkflowDesignScenarioGeneratorCoversEveryDeclaredBoundary is the
// WF-DISC-010 primary oracle: every template yields a scenario or a
// typed justification, RED-listed adversaries all appear, and oracles
// stay explicit.
func TestWorkflowDesignScenarioGeneratorCoversEveryDeclaredBoundary(t *testing.T) {
	matrix, findings := generateFixture(t)
	if len(findings) > 0 {
		t.Fatalf("clean generation raised findings: %+v", findings)
	}
	if matrix == nil {
		t.Fatal("clean generation produced no matrix")
	}
	if len(matrix.Scenarios)+len(matrix.NotApplicable) != TemplateCount {
		t.Fatalf("coverage = %d scenarios + %d justified, want all %d templates",
			len(matrix.Scenarios), len(matrix.NotApplicable), TemplateCount)
	}
	ids := make(map[string]bool)
	for _, scenario := range matrix.Scenarios {
		if scenario.ID == "" || scenario.Class == "" || scenario.Oracle == "" || scenario.Expected == "" || scenario.Prohibited == "" {
			t.Errorf("scenario %+v lacks an exact oracle", scenario)
		}
		if ids[scenario.ID] {
			t.Errorf("duplicate scenario id %s", scenario.ID)
		}
		ids[scenario.ID] = true
	}
	for _, na := range matrix.NotApplicable {
		if na.Class == "" || na.Reason == "" {
			t.Errorf("justification %+v untyped", na)
		}
	}
	for _, want := range []string{
		"stale-approval-revalidated", "partial-snapshot-detected", "conflicting-future-change",
		"duplicate-signal-coalesced", "provider-ambiguity-refused", "confidential-field-redacted",
		"cancellation-during-wait", "correction-supersedes", "partial-external-success", "failed-repair-escalates",
	} {
		if _, ok := scenarioNames(matrix)[want]; !ok {
			t.Errorf("RED adversary %s missing from the matrix", want)
		}
	}
	names := scenarioNames(matrix)
	if oracle := names["exact-write-posted"].Oracle; !strings.Contains(oracle, "manager-edge-replacement") {
		t.Errorf("write oracle not explicit: %q", oracle)
	}
	if oracle := names["stale-approval-revalidated"].Oracle; !strings.Contains(oracle, "approval-window") {
		t.Errorf("temporal oracle not explicit: %q", oracle)
	}
	if matrix.Digest == "" {
		t.Error("matrix carries no digest")
	}
	again, _ := generateFixture(t)
	if again.Digest != matrix.Digest {
		t.Error("matrix digest not deterministic")
	}
}

func TestTodo_WF_DISC_010_Property(t *testing.T) {
	t.Run("direct matrices justify inapplicable classes", func(t *testing.T) {
		record := matrixRecord()
		record.Disposition = workflowdesign.DispositionDirect
		record.Archetype = "D1"
		record.Writes = naDimension("READ_ONLY")
		record.Waits = naDimension("NO_WAIT")
		record.HumanWork = naDimension("NO_HUMAN_STEP")
		record.Invalidators = naDimension("NO_DEFERRAL")
		record.Correction = naDimension("NO_MUTATION")
		recipe := workflowexpansion.Recipe{Archetype: "D1", Phases: []string{
			"authenticate purpose and scope",
			"authorize fields and population",
			"execute deterministic read",
			"return typed result with trace",
		}}
		profile := workflowexpansion.Profile{Code: "PPL", Reads: "person facts", Writes: "people facts", Closure: "reconciliation"}
		graph, findings := workflowexpansion.Expand(workflowexpansion.Input{Recipe: recipe, Profile: profile, Record: record})
		if len(findings) > 0 {
			t.Fatalf("direct fixture failed expansion: %+v", findings)
		}
		matrix, genFindings := Generate(record, graph, profile)
		if len(genFindings) > 0 {
			t.Fatalf("direct generation raised findings: %+v", genFindings)
		}
		if len(matrix.Scenarios)+len(matrix.NotApplicable) != TemplateCount {
			t.Fatalf("direct coverage = %d + %d, want all %d templates",
				len(matrix.Scenarios), len(matrix.NotApplicable), TemplateCount)
		}
		reasons := make(map[string]bool)
		for _, na := range matrix.NotApplicable {
			reasons[na.Reason] = true
		}
		for _, want := range []string{"NO_WAIT", "READ_ONLY", "NO_HUMAN_STEP"} {
			if !reasons[want] {
				t.Errorf("direct matrix lacks the record's own %s justification", want)
			}
		}
		if _, ok := scenarioNames(matrix)["happy-path-execution"]; !ok {
			t.Error("direct matrix lost its happy path")
		}
	})
	t.Run("multi engines fan out", func(t *testing.T) {
		record := matrixRecord()
		record.Engines = workflowdesign.Dimension{Items: []string{"identity-resolution", "provenance-query"}}
		matrix, findings := Generate(record, matrixGraph(t, record), matrixProfile())
		if len(findings) > 0 {
			t.Fatalf("multi-engine raised findings: %+v", findings)
		}
		oracle := scenarioNames(matrix)["provider-ambiguity-refused"].Oracle
		if !strings.Contains(oracle, "identity-resolution") || !strings.Contains(oracle, "provenance-query") {
			t.Errorf("engine oracle not explicit: %q", oracle)
		}
	})
}

func TestTodo_WF_DISC_010_Golden(t *testing.T) {
	matrix, findings := generateFixture(t)
	if len(findings) > 0 {
		t.Fatalf("golden input raised findings: %+v", findings)
	}
	got, err := MarshalMatrix(matrix, findings)
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

func TestTodo_WF_DISC_010_Race(t *testing.T) {
	record := matrixRecord()
	graph := matrixGraph(t, record)
	profile := matrixProfile()
	const workers = 4
	results := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			matrix, findings := Generate(record, graph, profile)
			if len(findings) > 0 {
				results <- "error"
				return
			}
			results <- matrix.Digest
		}()
	}
	wg.Wait()
	close(results)
	want := ""
	for got := range results {
		if got == "error" {
			t.Fatal("concurrent generation raised findings")
		}
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("concurrent digest mismatch: %s then %s", want, got)
		}
	}
}

func TestTodo_WF_DISC_010_Fault(t *testing.T) {
	t.Run("nil graph fails closed", func(t *testing.T) {
		if matrix, findings := Generate(matrixRecord(), nil, matrixProfile()); matrix != nil || findingCodes(findings)[MissingExpansion] == 0 {
			t.Errorf("graphless generation produced a matrix or no finding: %+v", findings)
		}
	})
	t.Run("record graph mismatch fails closed", func(t *testing.T) {
		record := matrixRecord()
		record.Intent = "SomebodyElse"
		if matrix, findings := Generate(record, matrixGraph(t, matrixRecord()), matrixProfile()); matrix != nil || findingCodes(findings)[GraphMismatch] == 0 {
			t.Errorf("mismatched generation produced a matrix or no finding: %+v", findings)
		}
	})
	t.Run("profile mismatch fails closed", func(t *testing.T) {
		if matrix, findings := Generate(matrixRecord(), matrixGraph(t, matrixRecord()), workflowexpansion.Profile{Code: "ZZ"}); matrix != nil || findingCodes(findings)[ProfileMismatch] == 0 {
			t.Errorf("mismatched profile produced a matrix or no finding: %+v", findings)
		}
	})
}

func findingCodes(findings []Finding) map[string]int {
	codes := make(map[string]int)
	for _, finding := range findings {
		codes[finding.Code]++
	}
	return codes
}

func TestTodo_WF_DISC_010_Security(t *testing.T) {
	t.Run("every scenario carries a complete oracle", func(t *testing.T) {
		matrix, _ := generateFixture(t)
		for _, scenario := range matrix.Scenarios {
			if scenario.Oracle == "" || scenario.Expected == "" || scenario.Prohibited == "" || scenario.Fixture == "" {
				t.Errorf("scenario %s has an incomplete oracle", scenario.ID)
			}
		}
	})
	t.Run("leak coverage is always present", func(t *testing.T) {
		matrix, _ := generateFixture(t)
		leak, ok := scenarioNames(matrix)["confidential-field-redacted"]
		if !ok {
			t.Fatal("confidential-field template missing")
		}
		if !strings.Contains(leak.Prohibited, "leak") {
			t.Errorf("leak scenario prohibits nothing: %+v", leak)
		}
	})
	t.Run("justifications cannot be injected", func(t *testing.T) {
		matrix, _ := generateFixture(t)
		for _, na := range matrix.NotApplicable {
			if na.Reason == "" || na.Class == "" || na.Dimension == "" {
				t.Errorf("untyped justification: %+v", na)
			}
		}
		if len(matrix.Scenarios)+len(matrix.NotApplicable) != TemplateCount {
			t.Error("justification count escapes the template denominator")
		}
	})
}

func TestTodo_WF_DISC_010_Conformance(t *testing.T) {
	root := repoRoot(t)
	snap, err := workflowexpansion.LoadSnapshot(root)
	if err != nil {
		t.Fatalf("load live snapshot: %v", err)
	}
	counts := make(map[string]int)
	nas := 0
	for _, id := range snap.Accepted {
		graph, findings := workflowexpansion.ExpandSnapshot(snap, id, workflowexpansion.Delta{})
		if len(findings) > 0 {
			t.Fatalf("%s expansion failed: %+v", id, findings)
		}
		var record *workflowdesign.DesignRecord
		for i := range snap.Records {
			if snap.Records[i].Definition == id {
				record = &snap.Records[i]
				break
			}
		}
		if record == nil {
			t.Fatalf("%s has no design record", id)
		}
		matrix, genFindings := Generate(*record, graph, snap.Profiles[record.DomainProfile])
		if len(genFindings) > 0 {
			t.Errorf("%s generation raised findings: %+v", id, genFindings)
			continue
		}
		if len(matrix.Scenarios)+len(matrix.NotApplicable) != TemplateCount {
			t.Errorf("%s covers %d + %d, want all %d templates",
				id, len(matrix.Scenarios), len(matrix.NotApplicable), TemplateCount)
		}
		again, _ := Generate(*record, graph, snap.Profiles[record.DomainProfile])
		if again.Digest != matrix.Digest {
			t.Errorf("%s digest not deterministic", id)
		}
		for _, scenario := range matrix.Scenarios {
			counts[scenario.Class]++
		}
		nas += len(matrix.NotApplicable)
	}
	t.Logf("live matrices: classes %v justified %d", counts, nas)
}

func TestTodo_WF_DISC_010_Mutation(t *testing.T) {
	base, _ := generateFixture(t)
	t.Run("dropped writes justify write classes", func(t *testing.T) {
		record := matrixRecord()
		record.Writes = naDimension("READ_ONLY")
		matrix, findings := Generate(record, matrixGraph(t, record), matrixProfile())
		if len(findings) > 0 {
			t.Fatalf("read-only raised findings: %+v", findings)
		}
		if _, ok := scenarioNames(matrix)["exact-write-posted"]; ok {
			t.Error("write scenario survives dropped writes")
		}
		if matrix.Digest == base.Digest {
			t.Error("dropped writes left the digest unchanged")
		}
	})
	t.Run("flipped disposition justifies durability classes", func(t *testing.T) {
		record := matrixRecord()
		record.Disposition = workflowdesign.DispositionDirect
		record.Archetype = "D1"
		record.Writes = naDimension("READ_ONLY")
		record.Waits = naDimension("NO_WAIT")
		record.HumanWork = naDimension("NO_HUMAN_STEP")
		record.Invalidators = naDimension("NO_DEFERRAL")
		record.Correction = naDimension("NO_MUTATION")
		recipe := workflowexpansion.Recipe{Archetype: "D1", Phases: []string{"authenticate purpose", "return typed result"}}
		graph, findings := workflowexpansion.Expand(workflowexpansion.Input{Recipe: recipe, Profile: matrixProfile(), Record: record})
		if len(findings) > 0 {
			t.Fatalf("direct fixture failed expansion: %+v", findings)
		}
		matrix, genFindings := Generate(record, graph, matrixProfile())
		if len(genFindings) > 0 {
			t.Fatalf("direct generation raised findings: %+v", genFindings)
		}
		if _, ok := scenarioNames(matrix)["cancellation-during-wait"]; ok {
			t.Error("wait scenario survives dropped waits")
		}
	})
	t.Run("changed invalidator rewrites oracles", func(t *testing.T) {
		record := matrixRecord()
		record.Invalidators = workflowdesign.Dimension{Value: "band-revision"}
		matrix, findings := Generate(record, matrixGraph(t, record), matrixProfile())
		if len(findings) > 0 {
			t.Fatalf("reinvalidator raised findings: %+v", findings)
		}
		if oracle := scenarioNames(matrix)["invalidator-supersedes"].Oracle; !strings.Contains(oracle, "band-revision") {
			t.Errorf("oracle not rewritten: %q", oracle)
		}
		if matrix.Digest == base.Digest {
			t.Error("changed invalidator left the digest unchanged")
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
