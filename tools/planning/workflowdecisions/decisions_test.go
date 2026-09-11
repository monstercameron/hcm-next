package workflowdecisions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowModelingDecisionRejectsImplicitOrUnsafeDefault(t *testing.T) {
	entries, err := LoadRegister(filepath.Join("testdata", "fixture", "register.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("register entries = %d, want 2", len(entries))
	}
	if entries[0].ID != "unresolved-modeling-decision-1" || entries[0].Status != "" {
		t.Fatalf("first entry = %+v, want parsed open question", entries[0])
	}
	if entries[1].Question != "Which variants affect approvals, and their precedence." {
		t.Fatalf("multiline question = %q", entries[1].Question)
	}
	decisions, err := LoadSidecar(filepath.Join("testdata", "fixture", "decisions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 4 {
		t.Fatalf("sidecar decisions = %d, want 4", len(decisions))
	}
	report := ValidateDecisions(append(entries, decisions...), "2098-06-01")
	want := map[string][]string{
		"unresolved-modeling-decision-1": {"INVALID_STATUS", "MISSING_OWNER"},
		"contact-point-ownership":        {},
		"merge-policy":                   {},
		"rushed-ship":                    {"UNSAFE_DEFAULT", "UNSAFE_FIXTURE"},
		"stale-deferral":                 {"MISSING_INVALIDATION_TRIGGER", "OVERDUE_DECISION"},
	}
	for id, codes := range want {
		for _, code := range codes {
			if !hasFinding(report.Findings, id, code) {
				t.Errorf("missing %s for %s: %+v", code, id, report.Findings)
			}
		}
		if len(codes) == 0 {
			for _, finding := range report.Findings {
				if finding.Decision == id {
					t.Errorf("complete entry %s flagged: %+v", id, finding)
				}
			}
		}
	}
	second := ValidateDecisions(mustLoad(t), "2098-06-01")
	if report.Digest != second.Digest {
		t.Fatal("decision digest not deterministic")
	}
}

func mustLoad(t *testing.T) []Decision {
	t.Helper()
	entries, err := LoadRegister(filepath.Join("testdata", "fixture", "register.md"))
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := LoadSidecar(filepath.Join("testdata", "fixture", "decisions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return append(entries, decisions...)
}

func TestTodo_WF_DISC_003_Property(t *testing.T) {
	base := Decision{
		ID: "case", Question: "Q?", Status: "OPEN_OWNED", Owner: "people-domain",
		Deadline: "2099-01-01", Affected: []string{"contact-information"},
		SafeDefault: "ROUTE_HUMAN", Evidence: []string{"register.md"},
		LinkedTodos: []string{"WF-DISC-004"}, Revision: 1,
	}
	cases := []struct {
		name   string
		mutate func(*Decision)
		code   string
	}{
		{"invalid status", func(d *Decision) { d.Status = "SHIPPED" }, "INVALID_STATUS"},
		{"missing owner", func(d *Decision) { d.Owner = "  " }, "MISSING_OWNER"},
		{"bad deadline", func(d *Decision) { d.Deadline = "someday" }, "INVALID_DEADLINE"},
		{"missing affected", func(d *Decision) { d.Affected = nil }, "MISSING_AFFECTED"},
		{"missing default", func(d *Decision) { d.SafeDefault = "" }, "MISSING_SAFE_DEFAULT"},
		{"unsafe default", func(d *Decision) { d.SafeDefault = "PROCEED" }, "UNSAFE_DEFAULT"},
		{"missing evidence", func(d *Decision) { d.Evidence = nil }, "MISSING_EVIDENCE"},
		{"missing links", func(d *Decision) { d.LinkedTodos = nil }, "MISSING_LINKED_TODO"},
		{"overdue open", func(d *Decision) { d.Deadline = "2000-01-01" }, "OVERDUE_DECISION"},
		{"decided missing alternatives", func(d *Decision) {
			d.Status = "DECIDED"
			d.Revision = 1
			d.Consequences = "C"
			d.Authority = "A"
			d.InvalidationTrigger = "T"
			d.NegativeFixtures = []NegativeFixture{{Action: "a", Signal: "s", Expect: "ROUTE_HUMAN"}}
		}, "MISSING_ALTERNATIVES"},
		{"decided unproven fixture", func(d *Decision) {
			d.Status = "DECIDED"
			d.Revision = 1
			d.Alternatives = []string{"x"}
			d.Consequences = "C"
			d.Authority = "A"
			d.InvalidationTrigger = "T"
			d.NegativeFixtures = []NegativeFixture{{Action: "a", Signal: "s", Expect: "BLOCK"}}
		}, "UNSAFE_FIXTURE"},
		{"decided missing revision", func(d *Decision) {
			d.Status = "DECIDED"
			d.Revision = 0
			d.Alternatives = []string{"x"}
			d.Consequences = "C"
			d.Authority = "A"
			d.InvalidationTrigger = "T"
			d.NegativeFixtures = []NegativeFixture{{Action: "a", Signal: "s", Expect: "ROUTE_HUMAN"}}
		}, "MISSING_REVISION"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.mutate(&candidate)
			report := ValidateDecisions([]Decision{candidate}, "2099-12-31")
			if !hasFinding(report.Findings, "case", tc.code) {
				t.Errorf("mutation %s accepted: %+v", tc.name, report.Findings)
			}
		})
	}
}

func TestTodo_WF_DISC_003_Golden(t *testing.T) {
	report := ValidateDecisions(mustLoad(t), "2098-06-01")
	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "fixture", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_DISC_003_Security(t *testing.T) {
	forged := Decision{
		ID: "forged", Question: "Q?", Status: "DECIDED", Owner: "people-domain",
		Deadline: "2099-01-01", Affected: []string{"contact-information"},
		SafeDefault: "BLOCK", Revision: 1,
		Alternatives: []string{"x"}, Consequences: "C", Authority: "A",
		InvalidationTrigger: "T",
		NegativeFixtures:    []NegativeFixture{{Action: "a", Signal: "s", Expect: "BLOCK"}},
	}
	report := ValidateDecisions([]Decision{forged}, "2099-12-31")
	if !hasFinding(report.Findings, "forged", "MISSING_EVIDENCE") {
		t.Fatalf("evidenceless DECIDED accepted: %+v", report.Findings)
	}
	tampered := forged
	tampered.ID = "tampered"
	tampered.Evidence = []string{"register.md"}
	tampered.LinkedTests = []string{"TestTampered"}
	tampered.SafeDefault = "AUTO_APPROVE"
	report = ValidateDecisions([]Decision{tampered}, "2099-12-31")
	if !hasFinding(report.Findings, "tampered", "UNSAFE_DEFAULT") {
		t.Fatalf("tampered default accepted: %+v", report.Findings)
	}
	if !hasFinding(report.Findings, "tampered", "UNSAFE_FIXTURE") {
		t.Fatalf("fixture under tampered default accepted: %+v", report.Findings)
	}
}

func TestTodo_WF_DISC_003_Conformance(t *testing.T) {
	entries, err := LoadRegister(filepath.Join("..", "..", "..", "planning", "workflows", "hr-workflow-data-register.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 8 {
		t.Fatalf("live register entries = %d, want the 8 documented questions", len(entries))
	}
	report := ValidateDecisions(entries, "2099-12-31")
	counts := map[string]int{}
	for _, finding := range report.Findings {
		counts[finding.Code]++
	}
	want := map[string]int{
		"INVALID_STATUS": 8, "MISSING_OWNER": 8, "MISSING_DEADLINE": 8,
		"MISSING_AFFECTED": 8, "MISSING_SAFE_DEFAULT": 8, "MISSING_EVIDENCE": 8,
		"MISSING_LINKED_TODO": 8,
	}
	for code, count := range want {
		if counts[code] != count {
			t.Errorf("live %s = %d, want %d (full: %v)", code, counts[code], count, counts)
		}
	}
	for _, finding := range report.Findings {
		if finding.Code != "INVALID_STATUS" && finding.Code != "MISSING_OWNER" &&
			finding.Code != "MISSING_DEADLINE" && finding.Code != "MISSING_AFFECTED" &&
			finding.Code != "MISSING_SAFE_DEFAULT" && finding.Code != "MISSING_EVIDENCE" &&
			finding.Code != "MISSING_LINKED_TODO" {
			t.Fatalf("unexpected live finding: %+v", finding)
		}
	}
}

func TestTodo_WF_DISC_003_Mutation(t *testing.T) {
	before := ValidateDecisions(mustLoad(t), "2099-12-31")
	mutated := mustLoad(t)
	for i := range mutated {
		if mutated[i].ID == "contact-point-ownership" {
			mutated[i].Owner = ""
		}
		if mutated[i].ID == "merge-policy" {
			mutated[i].SafeDefault = "PROCEED"
		}
	}
	after := ValidateDecisions(mutated, "2099-12-31")
	if before.Digest == after.Digest {
		t.Fatal("owner removal and default tampering did not change the digest")
	}
	if !hasFinding(after.Findings, "contact-point-ownership", "MISSING_OWNER") {
		t.Fatalf("owner removal accepted: %+v", after.Findings)
	}
	if !hasFinding(after.Findings, "merge-policy", "UNSAFE_DEFAULT") {
		t.Fatalf("default tampering accepted: %+v", after.Findings)
	}
	dropped := mustLoad(t)[:4]
	short := ValidateDecisions(dropped, "2099-12-31")
	if short.Digest == before.Digest {
		t.Fatal("dropped entry did not change the digest")
	}
}

func hasFinding(findings []Finding, id, code string) bool {
	for _, finding := range findings {
		if (id == "" || finding.Decision == id) && finding.Code == code {
			return true
		}
	}
	return false
}
