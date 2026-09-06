package riskbinding

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestRiskBinding_DiagnosticMethodsAndLoads(t *testing.T) {
	finding := Finding{RiskID: "RISK-1", Kind: "DETECTION", Code: "MISSING", Detail: "evidence", Reason: "not covered"}
	if finding.Key() != "RISK-1|DETECTION|MISSING|evidence" {
		t.Fatalf("Finding.Key() = %q", finding.Key())
	}
	if finding.String() != "RISK-1: DETECTION/MISSING: not covered" {
		t.Fatalf("Finding.String() = %q", finding.String())
	}
	if (Finding{Code: "TABLE", Reason: "bad"}).String() != "<table>: TABLE: bad" {
		t.Fatalf("table Finding.String() did not use table label")
	}
	gap := Gap{RiskID: "RISK-1", Kind: Recovery, Code: "MISSING_RECOVERY", Detail: "x"}
	if gap.Key() != "RISK-1|RECOVERY|MISSING_RECOVERY|x" {
		t.Fatalf("Gap.Key() = %q", gap.Key())
	}
	report := Report{Findings: []Finding{{RiskID: "RISK-2", Code: "STRUCTURAL", Reason: "bad"}}, NewGaps: []Gap{{RiskID: "RISK-1", Kind: Recovery, Code: "MISSING_RECOVERY", Detail: "x"}}}
	violations := report.Violations()
	if len(violations) != 2 || !hasFinding(violations, "RISK-1", "MISSING_RECOVERY") || !hasFinding(violations, "RISK-2", "STRUCTURAL") {
		t.Fatalf("Report.Violations() = %#v", violations)
	}

	t.Run("load errors and version validation", func(t *testing.T) {
		if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil || !strings.Contains(err.Error(), "read risk table") {
			t.Fatalf("missing Load error = %v", err)
		}
		bad := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), "parse risk table") {
			t.Fatalf("malformed Load error = %v", err)
		}
		version := filepath.Join(t.TempDir(), "version.json")
		if err := os.WriteFile(version, []byte(`{"version":2}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(version); err == nil || !strings.Contains(err.Error(), "want 1") {
			t.Fatalf("version Load error = %v", err)
		}
	})

	data, err := (Table{Version: 1}).JSON()
	if err != nil || !strings.HasSuffix(string(data), "\n") || !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("Table.JSON() = %q, %v", data, err)
	}
}

func TestRiskBinding_EvaluateRejectsMalformedAndUnresolvedEvidence(t *testing.T) {
	table := Table{
		Version: 1,
		Risks: []Risk{
			{ID: "", Category: Architecture, Statement: "missing id", Owner: "owner", SourceDocument: "fixture", SourceLine: 1},
			{ID: "RISK-DUP", Category: Architecture, Statement: "first", Owner: "owner", SourceDocument: "fixture", SourceLine: 1},
			{ID: "RISK-DUP", Category: Product, Statement: "duplicate", Owner: "owner", SourceDocument: "fixture", SourceLine: 1},
			{ID: "RISK-BAD", Category: "OTHER", Statement: " ", Owner: "", SourceLine: 0},
			{ID: "RISK-GAPS", Category: Product, Statement: "gaps", Owner: "product", SourceDocument: "fixture", SourceLine: 1},
		},
		Bindings: []Binding{
			{RiskID: "UNKNOWN", Evidence: []Evidence{{Kind: Prevention, TestName: "TestPolicy"}}},
			{RiskID: "RISK-DUP", Evidence: []Evidence{{Kind: Prevention, TestName: "TestPolicy"}}},
			{RiskID: "RISK-DUP", Evidence: []Evidence{{Kind: Detection, TestName: "TestPolicy"}}},
			{RiskID: "RISK-GAPS", Evidence: []Evidence{
				{Kind: "OTHER", TestName: "TestPolicy"},
				{Kind: Prevention},
				{Kind: Detection, TodoID: "TODO-MISSING", TestName: "also-set"},
				{Kind: Recovery, TodoID: "TODO-UNKNOWN"},
				{Kind: Prevention, TodoID: "TODO-EMPTY"},
				{Kind: Detection, TestName: "TestMissing"},
			}},
		},
		Allowlist: []AllowlistedGap{
			{RiskID: "RISK-GAPS", Kind: string(Detection), Owner: "other-owner", Reason: "reviewed", ReviewDate: "2026-12-31", Status: Accepted},
			{RiskID: "RISK-GAPS", Kind: "OTHER", Owner: "product", Reason: "bad kind", ReviewDate: "2026-12-31"},
			{RiskID: "STALE", Kind: string(Recovery), Owner: "product", Reason: "stale", ReviewDate: "2026-12-31"},
		},
	}
	report := Evaluate(table, map[string]string{"TODO-EMPTY": ""}, map[string]bool{"TestPolicy": true})
	for _, expected := range []struct{ riskID, code string }{
		{"", "MISSING_RISK_ID"}, {"RISK-DUP", "DUPLICATE_RISK_ID"},
		{"RISK-BAD", "INVALID_CATEGORY"}, {"RISK-BAD", "INCOMPLETE_RISK"}, {"RISK-BAD", "MISSING_SOURCE_CITATION"},
		{"UNKNOWN", "UNKNOWN_RISK_BINDING"}, {"RISK-DUP", "DUPLICATE_RISK_BINDING"},
		{"RISK-GAPS", "INVALID_EVIDENCE_KIND"}, {"RISK-GAPS", "INVALID_EVIDENCE_REFERENCE"},
		{"RISK-GAPS", "UNRESOLVED_TODO"}, {"RISK-GAPS", "TODO_MISSING_TEST"}, {"RISK-GAPS", "UNRESOLVED_TEST"},
		{"RISK-GAPS", "INVALID_ALLOWLIST"}, {"RISK-GAPS", "ALLOWLIST_OWNER_MISMATCH"}, {"STALE", "STALE_ALLOWLIST"},
	} {
		if !hasFinding(report.Findings, expected.riskID, expected.code) {
			t.Errorf("missing %s/%s finding: %#v", expected.riskID, expected.code, report.Findings)
		}
	}
	if !hasGap(report.Gaps, "RISK-GAPS", Recovery) || len(report.AllowlistedGaps) != 1 || report.Rows[len(report.Rows)-1].Status == Mitigated {
		t.Fatalf("gap projection = gaps=%#v allowlisted=%#v rows=%#v", report.Gaps, report.AllowlistedGaps, report.Rows)
	}
}

func TestRiskBinding_DefaultEvidenceAndCitations(t *testing.T) {
	t.Run("default evidence binds an otherwise unbound risk and duplicate labels are deduplicated", func(t *testing.T) {
		table := Table{Version: 1, Risks: []Risk{{ID: "RISK-A", Category: Architecture, Statement: "A", Owner: "owner", SourceDocument: "fixture.md", SourceLine: 1}}, DefaultEvidence: []Evidence{{Kind: Prevention, TestName: "TestPolicy"}, {Kind: Prevention, TestName: "TestPolicy"}, {Kind: Detection, TodoID: "TODO-1"}, {Kind: Recovery, TodoID: "TODO-1"}}}
		report := Evaluate(table, map[string]string{"TODO-1": "TestTodo"}, map[string]bool{"TestPolicy": true})
		if len(report.Violations()) != 0 || len(report.Rows) != 1 || len(report.Rows[0].Prevention) != 1 || report.Rows[0].Status != Mitigated {
			t.Fatalf("default evidence report = %#v", report)
		}
	})

	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.md")
	if err := os.WriteFile(fixture, []byte("### Risk 1: Matching statement\n### Risk 2: Other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	risky := []Risk{
		{ID: "RISK-001", Statement: "Matching statement", SourceDocument: "fixture.md", SourceLine: 1},
		{ID: "RISK-002", Statement: "Wrong statement", SourceDocument: "fixture.md", SourceLine: 1},
		{ID: "RISK-003", Statement: "Other", SourceDocument: "fixture.md", SourceLine: 99},
		{ID: "RISK-004", Statement: "Missing", SourceDocument: "missing.md", SourceLine: 1},
	}
	findings := ValidateCitations(dir, risky)
	if len(findings) != 3 || !hasFinding(findings, "RISK-002", "SOURCE_LINE_MISMATCH") || !hasFinding(findings, "RISK-003", "SOURCE_LINE_MISMATCH") || !hasFinding(findings, "RISK-004", "SOURCE_DOCUMENT_UNREADABLE") {
		t.Fatalf("ValidateCitations = %#v", findings)
	}
}

func TestRiskBinding_RepositoryReaders(t *testing.T) {
	t.Run("todo registry and test scanner report file and parse errors", func(t *testing.T) {
		if _, err := LoadTodoTests(filepath.Join(t.TempDir(), "missing.json")); err == nil {
			t.Fatal("missing todo registry was accepted")
		}
		bad := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadTodoTests(bad); err == nil || !strings.Contains(err.Error(), "parse todo registry") {
			t.Fatalf("malformed todo registry error = %v", err)
		}
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "tools", "policy", "testdata"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "tools", "planning"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "tools", "policy", "policy_test.go"), []byte("package p\nfunc TestFound(t *T) {}\n// func TestComment(t *T) {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "tools", "policy", "testdata", "ignored_test.go"), []byte("package p\nfunc TestIgnored(t *T) {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		names, err := ScanTestNames(root)
		if err != nil || !names["TestFound"] || names["TestComment"] || names["TestIgnored"] {
			t.Fatalf("ScanTestNames = %#v, %v", names, err)
		}
		if _, err := ScanTestNames(filepath.Join(root, "missing")); err == nil {
			t.Fatal("missing test root was accepted")
		}
	})

	t.Run("repository evaluation propagates load errors and succeeds with empty registries", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "tools", "policy"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "tools", "planning"), 0o755); err != nil {
			t.Fatal(err)
		}
		tablePath := filepath.Join(root, "table.json")
		if err := os.WriteFile(tablePath, []byte(`{"version":1,"risks":[],"bindings":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		registryPath := filepath.Join(root, "registry.json")
		if err := os.WriteFile(registryPath, []byte(`[{"id":"TODO-1","test":"TestFound"}]`), 0o644); err != nil {
			t.Fatal(err)
		}
		report, err := EvaluateRepository(root, tablePath, registryPath)
		if err != nil || len(report.Findings) != 0 || len(report.Rows) != 0 {
			t.Fatalf("EvaluateRepository = %#v, %v", report, err)
		}
		if _, err := EvaluateRepository(root, filepath.Join(root, "missing.json"), registryPath); err == nil {
			t.Fatal("missing repository table was accepted")
		}
	})
}
