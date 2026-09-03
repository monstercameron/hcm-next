package runner_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/conformance/checks"
	"github.com/monstercameron/hcm-next/tools/conformance/discover"
	"github.com/monstercameron/hcm-next/tools/conformance/internal/reporoot"
	"github.com/monstercameron/hcm-next/tools/conformance/model"
	"github.com/monstercameron/hcm-next/tools/conformance/report"
	"github.com/monstercameron/hcm-next/tools/conformance/runner"
	"github.com/monstercameron/hcm-next/tools/conformance/vocab"
)

func loadVocabulary(t *testing.T) (*vocab.Vocabulary, string) {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	v, err := vocab.Load(filepath.Join(root, "planning", "specs", "workflow-runtime.md"))
	if err != nil {
		t.Fatalf("vocab.Load: %v", err)
	}
	return v, root
}

// promotionDocument parses the real reference workflow document the compiled
// plan is built from, so the test proves the executed section attaches to the
// document the conformance CLI actually walks.
func promotionDocument(t *testing.T) (*model.Workflow, []discover.FileResult, string) {
	t.Helper()
	v, root := loadVocabulary(t)
	refRoot := filepath.Join(root, "planning", "reference-workflows")
	results, err := discover.Documents(root, refRoot, v)
	if err != nil {
		t.Fatalf("discover.Documents: %v", err)
	}
	for _, res := range results {
		if res.Err != nil || res.Doc == nil {
			continue
		}
		for _, wf := range res.Doc.Workflows {
			if wf.ID == runner.PromotionDocumentID {
				return wf, results, refRoot
			}
		}
	}
	t.Fatalf("no parsed workflow with id %q under %s", runner.PromotionDocumentID, refRoot)
	return nil, nil, ""
}

// TestConformanceReportGainsAnExecutedReceiptSection is the CONF-001 seam
// closing: the same reference-workflow document that the document runner only
// parses is now also executed, and the report it feeds gains an
// executed-receipt section without its shape changing.
func TestConformanceReportGainsAnExecutedReceiptSection(t *testing.T) {
	v, root := loadVocabulary(t)
	wf, results, refRoot := promotionDocument(t)

	// The document-only report is the baseline: it carries no executed
	// section at all, which is what makes "gains" a claim rather than a label.
	baseline := report.Build(root, refRoot,
		filepath.Join(root, "planning", "specs", "workflow-runtime.md"),
		filepath.Join(root, "planning", "workflows", "_engine", "workflow-context-contract.md"),
		v, results, runner.DefaultFixedClock)
	baselineChecks := promotionChecks(t, baseline)
	for _, id := range runner.ExecutedCheckIDs() {
		if hasCheck(baselineChecks, id) {
			t.Fatalf("the document-only report already carries executed check %s", id)
		}
	}

	pr := runner.PlanRunner{Vocab: v}
	receipt, err := pr.Simulate(wf, runner.DefaultFixedClock)
	if err != nil {
		t.Fatalf("PlanRunner.Simulate: %v", err)
	}
	if receipt.ExecutionMode != "SIMULATE" {
		t.Errorf("execution mode = %q, want SIMULATE", receipt.ExecutionMode)
	}
	if len(receipt.Effects) != 0 {
		t.Errorf("Effects = %+v, want empty; executing a zero-effect plan performs no effect", receipt.Effects)
	}

	// Every document check the CONF-001 runner produced is still there: the
	// executed section is added evidence, not a replacement for the document
	// analysis that keeps the document and the plan honest about each other.
	documentOnly := runner.DocumentRunner{Vocab: v}
	plain, err := documentOnly.Simulate(wf, runner.DefaultFixedClock)
	if err != nil {
		t.Fatalf("DocumentRunner.Simulate: %v", err)
	}
	if len(receipt.Checks) != len(plain.Checks)+len(runner.ExecutedCheckIDs()) {
		t.Fatalf("plan runner produced %d checks, want %d document checks plus %d executed ones",
			len(receipt.Checks), len(plain.Checks), len(runner.ExecutedCheckIDs()))
	}
	for i, want := range plain.Checks {
		if receipt.Checks[i].ID != want.ID || receipt.Checks[i].Status != want.Status {
			t.Errorf("document check %d changed: got %s/%s, want %s/%s",
				i, receipt.Checks[i].ID, receipt.Checks[i].Status, want.ID, want.Status)
		}
	}
	for _, id := range runner.ExecutedCheckIDs() {
		result, ok := findCheck(receipt.Checks, id)
		if !ok {
			t.Fatalf("executed check %s is missing from the receipt", id)
		}
		if result.Status == checks.Fail {
			t.Errorf("executed check %s FAILED: %s", id, result.Detail)
		}
		if result.Detail == "" {
			t.Errorf("executed check %s carries no detail", id)
		}
		if len(result.Refs) == 0 {
			t.Errorf("executed check %s cites no spec reference", id)
		}
	}

	// The section says what actually ran, not what the document says should.
	trace, _ := findCheck(receipt.Checks, runner.CheckExecutedTrace)
	for _, node := range []string{"snapshot_worker", "simulate_compensation", "evaluate_band", "build_proposal", "raise_threshold"} {
		if !strings.Contains(trace.Detail, node) {
			t.Errorf("executed trace does not mention node %q: %s", node, trace.Detail)
		}
	}
	zero, _ := findCheck(receipt.Checks, runner.CheckZeroEffects)
	if zero.Status != checks.Pass {
		t.Errorf("executed run was not zero-effect: %s", zero.Detail)
	}
	lifecycle, _ := findCheck(receipt.Checks, runner.CheckLifecycle)
	for _, dimension := range []string{"RequestState=", "ExecutionState=", "BusinessState=", "ConsistencyState=", "ObligationState="} {
		if !strings.Contains(lifecycle.Detail, dimension) {
			t.Errorf("executed lifecycle omits %s: %s", dimension, lifecycle.Detail)
		}
	}

	// Rendering the executed checks through the report's own types puts the
	// section into the JSON and Markdown a consumer reads, with no change to
	// the report schema.
	rendered := renderReport(baseline, wf.ID, receipt.Checks)
	data, err := rendered.JSON()
	if err != nil {
		t.Fatalf("render report JSON: %v", err)
	}
	for _, id := range runner.ExecutedCheckIDs() {
		if !strings.Contains(string(data), id) {
			t.Errorf("rendered report JSON does not carry executed check %s", id)
		}
	}
	if !strings.Contains(string(rendered.Markdown()), runner.CheckExecuted) {
		t.Error("rendered report Markdown does not carry the executed-receipt section")
	}
}

// TestExecutedReceiptSectionIsDeterministic proves the executed section does
// not move the report off byte-stability: the whole point of the fixed clock
// is that two runs over the same inputs produce identical output.
func TestExecutedReceiptSectionIsDeterministic(t *testing.T) {
	v, _ := loadVocabulary(t)
	wf, _, _ := promotionDocument(t)

	pr := runner.PlanRunner{Vocab: v}
	first, err := pr.Simulate(wf, runner.DefaultFixedClock)
	if err != nil {
		t.Fatalf("first Simulate: %v", err)
	}
	second, err := pr.Simulate(wf, runner.DefaultFixedClock)
	if err != nil {
		t.Fatalf("second Simulate: %v", err)
	}
	if len(first.Checks) != len(second.Checks) {
		t.Fatalf("check counts differ: %d and %d", len(first.Checks), len(second.Checks))
	}
	for i := range first.Checks {
		if first.Checks[i].ID != second.Checks[i].ID ||
			first.Checks[i].Status != second.Checks[i].Status ||
			first.Checks[i].Detail != second.Checks[i].Detail {
			t.Fatalf("check %d drifted between runs:\n %+v\n %+v", i, first.Checks[i], second.Checks[i])
		}
	}
}

// TestPlanRunnerReportsAnUnboundDocumentAsUnknown proves a document with no
// compiled plan is a stated UNKNOWN rather than a silently missing section.
func TestPlanRunnerReportsAnUnboundDocumentAsUnknown(t *testing.T) {
	v, _ := loadVocabulary(t)
	pr := runner.PlanRunner{Vocab: v}

	receipt, err := pr.Simulate(&model.Workflow{
		ID:      "manager-change",
		Kind:    model.KindDocument,
		RawText: "# Manager Change\n",
	}, runner.DefaultFixedClock)
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	result, ok := findCheck(receipt.Checks, runner.CheckExecuted)
	if !ok {
		t.Fatal("no CompiledPlanExecuted check for an unbound document")
	}
	if result.Status != checks.Unknown {
		t.Errorf("status = %s, want UNKNOWN for a document with no compiled plan", result.Status)
	}
	if hasCheck(receipt.Checks, runner.CheckReceiptDigest) {
		t.Error("an unbound document produced a receipt digest check")
	}
}

func findCheck(results []checks.Result, id string) (checks.Result, bool) {
	for _, r := range results {
		if r.ID == id {
			return r, true
		}
	}
	return checks.Result{}, false
}

func hasCheck(results []checks.Result, id string) bool {
	_, ok := findCheck(results, id)
	return ok
}

// promotionChecks pulls the promotion workflow's checks out of a built report.
func promotionChecks(t *testing.T, rpt report.Report) []checks.Result {
	t.Helper()
	for _, doc := range rpt.Documents {
		for _, wf := range doc.Workflows {
			if wf.ID != runner.PromotionDocumentID {
				continue
			}
			out := make([]checks.Result, 0, len(wf.Checks))
			for _, c := range wf.Checks {
				out = append(out, checks.Result{ID: c.ID, Name: c.Name, Status: checks.Status(c.Status), Detail: c.Detail, Refs: c.Refs})
			}
			return out
		}
	}
	t.Fatalf("report carries no workflow %q", runner.PromotionDocumentID)
	return nil
}

// renderReport rebuilds a report with one workflow's checks replaced by the
// plan runner's, which is exactly the substitution the conformance command
// makes when it is pointed at a Runner that can execute.
func renderReport(base report.Report, workflowID string, results []checks.Result) report.Report {
	out := base
	out.Documents = append([]report.DocumentReport(nil), base.Documents...)
	for i := range out.Documents {
		workflows := append([]report.WorkflowReport(nil), out.Documents[i].Workflows...)
		for j := range workflows {
			if workflows[j].ID != workflowID {
				continue
			}
			workflows[j].Checks = nil
			workflows[j].Summary = report.Summary{}
			for _, c := range results {
				workflows[j].Checks = append(workflows[j].Checks, report.CheckResult{
					ID: c.ID, Name: c.Name, Status: string(c.Status), Detail: c.Detail, Refs: c.Refs,
				})
				switch c.Status {
				case checks.Pass:
					workflows[j].Summary.Pass++
				case checks.Fail:
					workflows[j].Summary.Fail++
				default:
					workflows[j].Summary.Unknown++
				}
			}
		}
		out.Documents[i].Workflows = workflows
	}
	return out
}
