package directcapability

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion"
)

func naDimension(reason string) workflowdesign.Dimension {
	return workflowdesign.Dimension{Value: workflowdesign.NotApplicable, Reason: reason}
}

func directRecord() workflowdesign.DesignRecord {
	return workflowdesign.DesignRecord{
		Intent:         "ExplainTransaction",
		Definition:     "hcmnext.intelligence.explain_transaction/v1",
		Disposition:    workflowdesign.DispositionDirect,
		Archetype:      "D1",
		DomainProfile:  "ANALYTICS",
		InputBoundary:  "explanation-request",
		SnapshotPolicy: "pinned-rule-versions",
		Engines:        workflowdesign.Dimension{Items: []string{"provenance-query"}},
		HumanWork:      naDimension("NO_HUMAN_STEP"),
		Writes:         naDimension("READ_ONLY"),
		Waits:          naDimension("NO_WAIT"),
		Invalidators:   naDimension("NO_DEFERRAL"),
		Reconciliation: naDimension("NO_EXTERNAL_EFFECT"),
		Correction:     naDimension("NO_MUTATION"),
		Completion:     "explanation-delivered",
	}
}

func directRecipe() workflowexpansion.Recipe {
	return workflowexpansion.Recipe{Archetype: "D1", Phases: []string{
		"authenticate purpose and scope",
		"authorize fields and population",
		"execute deterministic read",
		"return typed result with trace",
	}}
}

func directProfile() workflowexpansion.Profile {
	return workflowexpansion.Profile{Code: "ANALYTICS", Reads: "governed model", Writes: "result only", Closure: "reproducible result"}
}

func directGraph(t *testing.T, record workflowdesign.DesignRecord) *workflowexpansion.Graph {
	t.Helper()
	graph, findings := workflowexpansion.Expand(workflowexpansion.Input{Recipe: directRecipe(), Profile: directProfile(), Record: record})
	if len(findings) > 0 {
		t.Fatalf("direct fixture failed expansion: %+v", findings)
	}
	return graph
}

func governedExecution(t *testing.T) Execution {
	t.Helper()
	record := directRecord()
	return Execution{
		Record: record,
		Graph:  directGraph(t, record),
		Auth: Authorization{
			Purpose:    "explain one transaction to its auditor",
			Fields:     []string{"amount", "timestamp"},
			Population: []string{"single-transaction"},
			Rules:      "pinned-rule-versions",
			Watermark:  "ledger-watermark-42",
			DlpPolicy:  "field-redaction-v3",
		},
		Result: DirectResult{
			Completeness: CompletenessComplete,
			Uncertainty:  "none: single pinned source",
			Trace:        "trace-9f2",
			Evidence:     "evidence-9f2",
		},
		Actions: []ActionProposal{{IntentRef: "hcmnext.operations.create_repair_plan/v1", Governed: true, Summary: "open a repair plan"}},
	}
}

func findingCodes(findings []Finding) map[string]int {
	codes := make(map[string]int)
	for _, finding := range findings {
		codes[finding.Code]++
	}
	return codes
}

// TestDirectCapabilityDispositionIsPureGovernedAndPromotesActionsAsNewIntents
// is the WF-DISC-008 primary oracle: a governed DIRECT execution proves
// pure, while every RED shape fails with its exact code.
func TestDirectCapabilityDispositionIsPureGovernedAndPromotesActionsAsNewIntents(t *testing.T) {
	if findings := ProveExecution(governedExecution(t)); len(findings) > 0 {
		t.Fatalf("governed execution failed proof: %+v", findings)
	}
	record := directRecord()
	if findings := ProveDesign(record, directGraph(t, record)); len(findings) > 0 {
		t.Fatalf("pure design failed proof: %+v", findings)
	}

	waits := governedExecution(t)
	waits.Record.Waits = workflowdesign.Dimension{Value: "approval-window"}
	if findingCodes(ProveExecution(waits))[DurableMechanism] == 0 {
		t.Error("DIRECT waits accepted")
	}
	human := governedExecution(t)
	human.Record.HumanWork = workflowdesign.Dimension{Value: "manager-self-service"}
	if findingCodes(ProveExecution(human))[DurableMechanism] == 0 {
		t.Error("DIRECT human work accepted")
	}
	writes := governedExecution(t)
	writes.Record.Writes = workflowdesign.Dimension{Value: "domain-row"}
	if findingCodes(ProveExecution(writes))[StateMutation] == 0 {
		t.Error("DIRECT writes accepted")
	}
	purpose := governedExecution(t)
	purpose.Auth.Purpose = ""
	if findingCodes(ProveExecution(purpose))[MissingPurpose] == 0 {
		t.Error("purposeless execution accepted")
	}
	complete := governedExecution(t)
	complete.Result.Exclusions = []string{"redacted-field"}
	if findingCodes(ProveExecution(complete))[ContradictoryCompleteness] == 0 {
		t.Error("complete-with-exclusions accepted")
	}
	executed := governedExecution(t)
	executed.Actions[0].Executed = true
	if findingCodes(ProveExecution(executed))[ExecutedProposal] == 0 {
		t.Error("executed proposal accepted")
	}
	ungoverned := governedExecution(t)
	ungoverned.Actions[0] = ActionProposal{Summary: "fix it directly"}
	if findingCodes(ProveExecution(ungoverned))[UngovernedAction] == 0 {
		t.Error("ungoverned action accepted")
	}
	timer := governedExecution(t)
	timer.Graph.Nodes = append(timer.Graph.Nodes, timer.Graph.Nodes[0])
	timer.Graph.Nodes[len(timer.Graph.Nodes)-1].Responsibility = "retry after timer deadline"
	if findingCodes(ProveExecution(timer))[DurableMechanism] == 0 {
		t.Error("timer node accepted")
	}
}

func TestTodo_WF_DISC_008_Property(t *testing.T) {
	t.Run("partial completeness tolerates exclusions", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Result.Completeness = CompletenessPartial
		exec.Result.Exclusions = []string{"redacted-field"}
		if findings := ProveExecution(exec); len(findings) > 0 {
			t.Errorf("honest partial result refused: %+v", findings)
		}
	})
	t.Run("unknown completeness tolerates silence", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Result.Completeness = CompletenessUnknown
		if findings := ProveExecution(exec); len(findings) > 0 {
			t.Errorf("honest unknown result refused: %+v", findings)
		}
	})
	t.Run("empty actions prove", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Actions = nil
		if findings := ProveExecution(exec); len(findings) > 0 {
			t.Errorf("action-free execution refused: %+v", findings)
		}
	})
	t.Run("each authorization dimension is required", func(t *testing.T) {
		cases := []struct {
			name  string
			strip func(*Authorization)
			code  string
		}{
			{"fields", func(a *Authorization) { a.Fields = nil }, MissingFieldScope},
			{"population", func(a *Authorization) { a.Population = nil }, MissingPopulationScope},
			{"rules", func(a *Authorization) { a.Rules = "" }, MissingRulePin},
			{"watermark", func(a *Authorization) { a.Watermark = "" }, MissingWatermark},
			{"dlp", func(a *Authorization) { a.DlpPolicy = "" }, MissingDlpPolicy},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				exec := governedExecution(t)
				tc.strip(&exec.Auth)
				if findingCodes(ProveExecution(exec))[tc.code] == 0 {
					t.Errorf("stripped %s accepted", tc.name)
				}
			})
		}
	})
	t.Run("each result dimension is required", func(t *testing.T) {
		cases := []struct {
			name  string
			strip func(*DirectResult)
			code  string
		}{
			{"completeness", func(r *DirectResult) { r.Completeness = "" }, MissingCompleteness},
			{"uncertainty", func(r *DirectResult) { r.Uncertainty = "" }, MissingUncertainty},
			{"trace", func(r *DirectResult) { r.Trace = "" }, MissingTrace},
			{"evidence", func(r *DirectResult) { r.Evidence = "" }, MissingEvidence},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				exec := governedExecution(t)
				tc.strip(&exec.Result)
				if findingCodes(ProveExecution(exec))[tc.code] == 0 {
					t.Errorf("stripped %s accepted", tc.name)
				}
			})
		}
	})
	t.Run("nil graph fails closed", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Graph = nil
		if findingCodes(ProveExecution(exec))[MissingExpansion] == 0 {
			t.Error("graphless execution accepted")
		}
	})
}

func TestTodo_WF_DISC_008_Golden(t *testing.T) {
	exec := governedExecution(t)
	exec.Auth.Watermark = ""
	exec.Actions[0].Executed = true
	findings := ProveExecution(exec)
	got, err := MarshalFindings(findings)
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

func TestTodo_WF_DISC_008_Fault(t *testing.T) {
	t.Run("nil graph fails closed", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Graph = nil
		if findings := ProveExecution(exec); findingCodes(findings)[MissingExpansion] == 0 {
			t.Errorf("graphless execution proved: %+v", findings)
		}
	})
	t.Run("record graph mismatch fails closed", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Record.Intent = "SomebodyElse"
		if findings := ProveExecution(exec); findingCodes(findings)[GraphMismatch] == 0 {
			t.Errorf("mismatched execution proved: %+v", findings)
		}
	})
	t.Run("empty result fails closed", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Result = DirectResult{}
		findings := ProveExecution(exec)
		for _, code := range []string{MissingCompleteness, MissingUncertainty, MissingTrace, MissingEvidence} {
			if findingCodes(findings)[code] == 0 {
				t.Errorf("empty result lacks %s: %+v", code, findings)
			}
		}
	})
	t.Run("tampered graph fails closed", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Graph.Edges = exec.Graph.Edges[:1]
		if findings := ProveExecution(exec); findingCodes(findings)[GraphInvalid] == 0 {
			t.Errorf("tampered graph proved: %+v", findings)
		}
	})
}

func TestTodo_WF_DISC_008_Security(t *testing.T) {
	t.Run("smuggled writes fail even with a clean graph", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Record.Writes = workflowdesign.Dimension{Items: []string{"hidden-row"}}
		if findingCodes(ProveExecution(exec))[StateMutation] == 0 {
			t.Error("smuggled writes accepted")
		}
	})
	t.Run("partial authorization fails", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Auth = Authorization{Purpose: "vibes"}
		findings := ProveExecution(exec)
		for _, code := range []string{MissingFieldScope, MissingPopulationScope, MissingRulePin, MissingWatermark, MissingDlpPolicy} {
			if findingCodes(findings)[code] == 0 {
				t.Errorf("partial auth lacks %s: %+v", code, findings)
			}
		}
	})
	t.Run("forged completeness fails", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Result.Completeness = CompletenessComplete
		exec.Result.Exclusions = []string{"everything"}
		exec.Result.Uncertainty = "none whatsoever"
		if findingCodes(ProveExecution(exec))[ContradictoryCompleteness] == 0 {
			t.Error("forged completeness accepted")
		}
	})
	t.Run("executed but governed proposal still fails", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Actions[0].Governed = true
		exec.Actions[0].Executed = true
		if findingCodes(ProveExecution(exec))[ExecutedProposal] == 0 {
			t.Error("executed proposal accepted")
		}
	})
	t.Run("workflow disposition never proves direct", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Record.Disposition = workflowdesign.DispositionWorkflow
		if findingCodes(ProveExecution(exec))[WrongDisposition] == 0 {
			t.Error("workflow record proved direct")
		}
	})
}

func TestTodo_WF_DISC_008_Conformance(t *testing.T) {
	root := repoRoot(t)
	snap, err := workflowexpansion.LoadSnapshot(root)
	if err != nil {
		t.Fatalf("load live snapshot: %v", err)
	}
	direct := 0
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
		if record.Disposition != workflowdesign.DispositionDirect {
			continue
		}
		direct++
		if proof := ProveDesign(*record, graph); len(proof) > 0 {
			t.Errorf("%s design impure: %+v", id, proof)
		}
		again, _ := workflowexpansion.ExpandSnapshot(snap, id, workflowexpansion.Delta{})
		_ = again
	}
	if direct != 5 {
		t.Errorf("direct records = %d, want 5", direct)
	}
	t.Logf("live direct purity: %d DIRECT designs prove pure", direct)
}

func TestTodo_WF_DISC_008_Mutation(t *testing.T) {
	t.Run("added waits break purity", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Record.Waits = workflowdesign.Dimension{Value: "timer-window"}
		if findingCodes(ProveExecution(exec))[DurableMechanism] == 0 {
			t.Error("added waits accepted")
		}
	})
	t.Run("dropped watermark breaks governance", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Auth.Watermark = ""
		if findingCodes(ProveExecution(exec))[MissingWatermark] == 0 {
			t.Error("dropped watermark accepted")
		}
	})
	t.Run("flipped proposal breaks promotion", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Actions[0].Executed = true
		if findingCodes(ProveExecution(exec))[ExecutedProposal] == 0 {
			t.Error("flipped proposal accepted")
		}
	})
	t.Run("stripped trace breaks honesty", func(t *testing.T) {
		exec := governedExecution(t)
		exec.Result.Trace = ""
		if findingCodes(ProveExecution(exec))[MissingTrace] == 0 {
			t.Error("stripped trace accepted")
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
