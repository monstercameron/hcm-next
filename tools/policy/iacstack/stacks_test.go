package iacstack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestTodo_IAC_002(t *testing.T) {
	stacks := StandardStacks()
	if len(stacks) != 4 {
		t.Fatalf("standard stacks = %d, want dev, test, stage and production-cell", len(stacks))
	}
	if report := ValidateStacks(stacks, ReviewedVariableSet()); !report.OK() {
		t.Fatalf("standard stacks rejected: %+v", report.Findings)
	}
	summary := SummarizeStacks(stacks)
	if len(summary.Lines) == 0 || summary.Digest == "" {
		t.Fatalf("empty plan summary: %+v", summary)
	}
	again := SummarizeStacks(stacks)
	if summary.Digest != again.Digest {
		t.Fatalf("plan summary not deterministic: %s vs %s", summary.Digest, again.Digest)
	}

	undocumented := append(append([]Stack(nil), stacks...), Stack{Name: "shadow", Modules: stacks[0].Modules})
	if report := ValidateStacks(undocumented, ReviewedVariableSet()); !hasStackFinding(report, "UNKNOWN_STACK", "shadow") {
		t.Fatalf("undocumented environment accepted: %+v", report.Findings)
	}

	shared := append([]Stack(nil), stacks...)
	shared[1].CredentialIDs = append([]string(nil), stacks[0].CredentialIDs...)
	if report := ValidateStacks(shared, ReviewedVariableSet()); !hasStackFinding(report, "SHARED_CREDENTIAL", "") {
		t.Fatalf("shared credentials accepted: %+v", report.Findings)
	}
}

func TestTodo_IAC_002_Property(t *testing.T) {
	stacks := StandardStacks()
	want := SummarizeStacks(stacks).Digest
	reversed := append([]Stack(nil), stacks...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	for _, stack := range reversed {
		for i, j := 0, len(stack.Modules)-1; i < j; i, j = i+1, j-1 {
			stack.Modules[i], stack.Modules[j] = stack.Modules[j], stack.Modules[i]
		}
	}
	if got := SummarizeStacks(reversed).Digest; got != want {
		t.Fatalf("plan digest depends on declaration order: %s vs %s", got, want)
	}
	if report := ValidateStacks(reversed, ReviewedVariableSet()); !report.OK() {
		t.Fatalf("reordered stacks rejected: %+v", report.Findings)
	}

	cases := []struct {
		name   string
		mutate func(*Stack)
		code   string
	}{
		{"unpinned version", func(s *Stack) { s.Modules[0].Version = "" }, "UNPINNED_MODULE"},
		{"unpinned digest", func(s *Stack) { s.Modules[0].Digest = "" }, "UNPINNED_MODULE"},
		{"malformed digest", func(s *Stack) { s.Modules[0].Digest = "sha256:xyz" }, "UNPINNED_MODULE"},
		{"unreviewed variable", func(s *Stack) { s.Variables["manual_override"] = "yes" }, "UNREVIEWED_VARIABLE"},
		{"literal sensitive value", func(s *Stack) { s.Variables["db_password"] = "hunter2" }, "LITERAL_SENSITIVE_VALUE"},
		{"missing bounds", func(s *Stack) { s.Cost = CostBounds{} }, "MISSING_BOUNDS"},
		{"missing marking", func(s *Stack) { s.NonProductionMark = false }, "MISSING_NONPROD_MARKING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// StandardStacks orders dev first, so index 0 is non-production.
			mutated := append([]Stack(nil), stacks...)
			target := mutated[0]
			tc.mutate(&target)
			mutated[0] = target
			if report := ValidateStacks(mutated, ReviewedVariableSet()); !hasStackFinding(report, tc.code, "") {
				t.Errorf("mutation %s accepted: %+v", tc.name, report.Findings)
			}
		})
	}
}

func TestTodo_IAC_002_Golden(t *testing.T) {
	summary := SummarizeStacks(StandardStacks())
	raw, err := json.Marshal(summary.Lines)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(strings.Join(decoded, "\n")))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if summary.Digest != want {
		t.Fatalf("digest = %s, want binding over exact lines %s", summary.Digest, want)
	}
	for _, line := range summary.Lines {
		if strings.Contains(line, "hunter2") || strings.Contains(line, "ref://") {
			t.Fatalf("plan summary leaks credential material: %q", line)
		}
	}
}

func TestTodo_IAC_002_Integration(t *testing.T) {
	stacks := StandardStacks()
	if report := ValidateStacks(stacks, ReviewedVariableSet()); !report.OK() {
		t.Fatalf("standard stacks rejected: %+v", report.Findings)
	}
	summary := SummarizeStacks(stacks)
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PlanSummary
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Digest != summary.Digest || len(decoded.Lines) != len(summary.Lines) {
		t.Fatalf("summary did not survive serialization: %+v", decoded)
	}
	if report := ValidateStacks(stacks, ReviewedVariableSet()); !report.OK() {
		t.Fatalf("revalidated stacks rejected: %+v", report.Findings)
	}

	diverged := append([]Stack(nil), stacks...)
	for i := range diverged {
		if diverged[i].Name == "production-cell" {
			diverged[i].Modules[0].Version = "9.9.9"
		}
	}
	if report := ValidateStacks(diverged, ReviewedVariableSet()); !hasStackFinding(report, "MODULE_GRAPH_DIVERGENCE", "") {
		t.Fatalf("diverged module graph accepted: %+v", report.Findings)
	}
}

func TestTodo_IAC_002_Security(t *testing.T) {
	stacks := StandardStacks()

	leak := append([]Stack(nil), stacks...)
	for i := range leak {
		if !leak[i].Production {
			leak[i].DataClass = "production"
			break
		}
	}
	if report := ValidateStacks(leak, ReviewedVariableSet()); !hasStackFinding(report, "PRODUCTION_DATA_IN_NONPROD", "") {
		t.Fatalf("production data in non-prod accepted: %+v", report.Findings)
	}

	unmarked := append([]Stack(nil), stacks...)
	for i := range unmarked {
		if !unmarked[i].Production {
			unmarked[i].NonProductionMark = false
			break
		}
	}
	if report := ValidateStacks(unmarked, ReviewedVariableSet()); !hasStackFinding(report, "MISSING_NONPROD_MARKING", "") {
		t.Fatalf("unmarked non-prod stack accepted: %+v", report.Findings)
	}

	fake := append([]Stack(nil), stacks...)
	fake[0].Modules[0].Digest = "sha256:" + strings.Repeat("0", 64)
	if report := ValidateStacks(fake, ReviewedVariableSet()); !hasStackFinding(report, "MODULE_GRAPH_DIVERGENCE", "") {
		t.Fatalf("forged module digest accepted: %+v", report.Findings)
	}
}

func hasStackFinding(report Report, code, stack string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code && (stack == "" || finding.Stack == stack) {
			return true
		}
	}
	return false
}
