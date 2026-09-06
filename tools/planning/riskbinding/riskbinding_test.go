package riskbinding

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRiskRegisterRejectsUncontrolledOrUntestedRisk(t *testing.T) {
	table, err := Load(filepath.Join("testdata", "risk-table.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Risks) != 141 {
		t.Fatalf("risk count = %d, want 141", len(table.Risks))
	}
	root := repoRoot(t)
	report, err := EvaluateRepository(root, filepath.Join(root, "tools", "planning", "riskbinding", "testdata", "risk-table.json"), filepath.Join(root, "definitions", "planning", "todo-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Violations(); len(got) != 0 {
		t.Fatalf("live risk register violations = %v", got)
	}
	if len(report.AllowlistedGaps) != 2 {
		t.Fatalf("allowlisted gaps = %d, want 2", len(report.AllowlistedGaps))
	}
	if report.ByCategory[Architecture].Unbound != 0 || report.ByCategory[Product].Unbound != 2 {
		t.Fatalf("unbound by category = %+v, want architecture 0 and product 2", report.ByCategory)
	}

	mutated := validTable()
	mutated.Bindings[0].Evidence = mutated.Bindings[0].Evidence[:2]
	mutated.Bindings[1].Evidence[1].TestName = "TestMissingPolicyEvidence"
	report = Evaluate(mutated, map[string]string{"GOV-023": "TestRiskRegisterRejectsUncontrolledOrUntestedRisk"}, map[string]bool{"TestPolicy": true})
	if len(report.Violations()) == 0 {
		t.Fatal("uncontrolled or untested risk was accepted")
	}
	if !hasGap(report.NewGaps, "RISK-A", Recovery) {
		t.Fatalf("missing recovery was not reported: %v", report.Findings)
	}
	if !hasFinding(report.Findings, "RISK-B", "UNRESOLVED_TEST") {
		t.Fatalf("unresolved policy test was not reported: %v", report.Findings)
	}
}

func TestTodo_GOV_023_Property(t *testing.T) {
	first := Evaluate(validTable(), todoTests(), map[string]bool{"TestPolicy": true})
	second := Evaluate(validTable(), todoTests(), map[string]bool{"TestPolicy": true})
	left, err := first.BindingTableJSON()
	if err != nil {
		t.Fatal(err)
	}
	right, err := second.BindingTableJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("risk binding evaluation is not deterministic")
	}
}

func TestTodo_GOV_023_Golden(t *testing.T) {
	report := Evaluate(validTable(), todoTests(), map[string]bool{"TestPolicy": true})
	got, err := report.BindingTableJSON()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "binding-table.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("binding table drifted:\n got %s\nwant %s", got, want)
	}
}

func TestTodo_GOV_023_Fault(t *testing.T) {
	table := validTable()
	table.Bindings[0].Evidence = table.Bindings[0].Evidence[:2]
	report := Evaluate(table, todoTests(), map[string]bool{"TestPolicy": true})
	if len(report.NewGaps) != 1 || report.NewGaps[0].RiskID != "RISK-A" || report.NewGaps[0].Kind != Recovery {
		t.Fatalf("fault gap = %+v, want one RISK-A recovery gap", report.NewGaps)
	}
	if report.ByCategory[Architecture].NewGapCount != 1 {
		t.Fatalf("architecture new gap count = %d, want 1", report.ByCategory[Architecture].NewGapCount)
	}
}

func TestTodo_GOV_023_Security(t *testing.T) {
	table := validTable()
	table.Bindings[0].Evidence[1] = Evidence{Kind: Detection, TestName: "TestNotInPolicyTree"}
	report := Evaluate(table, todoTests(), map[string]bool{"TestPolicy": true})
	if !hasFinding(report.Findings, "RISK-A", "UNRESOLVED_TEST") {
		t.Fatalf("unresolved policy evidence missing: %v", report.Findings)
	}
	if report.Rows[0].Detection != nil {
		t.Fatalf("unresolved security evidence was exposed in row: %+v", report.Rows[0])
	}
}

func TestTodo_GOV_023_Conformance(t *testing.T) {
	table := validTable()
	report := Evaluate(table, map[string]string{"GOV-023": "TestTodo"}, map[string]bool{"TestPolicy": true})
	if len(report.Violations()) != 0 {
		t.Fatalf("todo and policy evidence did not conform: %v", report.Violations())
	}
	if report.Rows[0].Prevention[0] != "todo:GOV-023=TestTodo" || report.Rows[0].Detection[0] != "test:TestPolicy" {
		t.Fatalf("evidence projection = %+v", report.Rows[0])
	}
}

func TestTodo_GOV_023_Mutation(t *testing.T) {
	table := validTable()
	table.Bindings[1].Evidence = table.Bindings[1].Evidence[:1]
	report := Evaluate(table, todoTests(), map[string]bool{"TestPolicy": true})
	if len(report.Violations()) == 0 {
		t.Fatal("removing a detection and recovery binding did not block the gate")
	}
	if !hasGap(report.NewGaps, "RISK-B", Detection) || !hasGap(report.NewGaps, "RISK-B", Recovery) {
		t.Fatalf("mutation gaps = %v", report.NewGaps)
	}
}

func validTable() Table {
	return Table{
		Version: 1,
		Risks: []Risk{
			{ID: "RISK-A", Category: Architecture, Statement: "Architecture fixture", Owner: "architecture-owner", SourceDocument: "fixture.md", SourceLine: 1},
			{ID: "RISK-B", Category: Product, Statement: "Product fixture", Owner: "product-owner", SourceDocument: "fixture.md", SourceLine: 2},
		},
		Bindings: []Binding{
			{RiskID: "RISK-A", Evidence: []Evidence{{Kind: Prevention, TodoID: "GOV-023"}, {Kind: Detection, TestName: "TestPolicy"}, {Kind: Recovery, TodoID: "GOV-023"}}},
			{RiskID: "RISK-B", Evidence: []Evidence{{Kind: Prevention, TodoID: "GOV-023"}, {Kind: Detection, TestName: "TestPolicy"}, {Kind: Recovery, TodoID: "GOV-023"}}},
		},
	}
}

func todoTests() map[string]string {
	return map[string]string{"GOV-023": "TestRiskRegisterRejectsUncontrolledOrUntestedRisk"}
}

func hasFinding(findings []Finding, riskID, code string) bool {
	for _, finding := range findings {
		if finding.RiskID == riskID && finding.Code == code {
			return true
		}
	}
	return false
}

func hasGap(gaps []Gap, riskID string, kind EvidenceKind) bool {
	for _, gap := range gaps {
		if gap.RiskID == riskID && gap.Kind == kind {
			return true
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve package path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}
