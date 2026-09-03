package plancontradiction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanningContradictions is the primary red/green test for GOV-014.
func TestPlanningContradictions(t *testing.T) {
	t.Run("a later spec broadening Phase 1 is rejected", func(t *testing.T) {
		v := CheckPlanningContradictions("Phase 1 now implements Hire and Onboard alongside Promotion.")
		assertHasRule(t, v, "later spec broadens Phase 1 beyond Promotion + Compensation Change")
	})
	t.Run("conditional messaging marked implemented is rejected", func(t *testing.T) {
		v := CheckPlanningContradictions("The Messaging and Notification Plane is implemented for the pilot tenant.")
		assertHasRule(t, v, "conditional messaging marked implemented")
	})
	t.Run("all reference workflows treated as product scope is rejected", func(t *testing.T) {
		v := CheckPlanningContradictions("All five reference workflows are in scope for the pilot release.")
		assertHasRule(t, v, "all reference workflows treated as product scope")
	})

	t.Run("the correct authoritative prose is not rejected", func(t *testing.T) {
		correct := []string{
			"Prove that HCM Next improves one Promotion + Compensation Change workflow for paid design partners.",
			"The five lifecycle workflows and payroll-correction stress test remain architecture conformance tests, not a Phase 1 feature roadmap.",
			"The Messaging and Notification Plane will turn workflow communication intent into policy-governed human delivery.",
		}
		for _, line := range correct {
			if v := CheckPlanningContradictions(line); len(v) != 0 {
				t.Errorf("correct prose incorrectly flagged: %q -> %v", line, v)
			}
		}
	})

	t.Run("a checker's own RED field description is not rejected", func(t *testing.T) {
		line := "  - **RED:** `TestPlanningContradictions` detects a later spec that broadens Phase 1, marks conditional messaging implemented, or treats all reference workflows as product scope."
		if v := CheckPlanningContradictions(line); len(v) != 0 {
			t.Errorf("spec-description RED field incorrectly flagged: %v", v)
		}
	})

	t.Run("real planning corpus has zero unresolved plan contradictions", func(t *testing.T) {
		total := scanRealCorpus(t)
		if total != 0 {
			t.Fatalf("found %d plan-contradiction violations in the real planning corpus", total)
		}
	})
}

func scanRealCorpus(t *testing.T) int {
	t.Helper()
	root := filepath.Join("..", "..", "..", "planning")
	var total int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, v := range CheckPlanningContradictions(string(content)) {
			total++
			t.Errorf("%s: %s", path, v)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk planning/: %v", err)
	}
	return total
}

func assertHasRule(t *testing.T, violations []Violation, rule string) {
	t.Helper()
	for _, v := range violations {
		if v.Rule == rule {
			return
		}
	}
	t.Errorf("expected a violation for rule %q, got %v", rule, violations)
}

// TestTodo_GOV_014_Golden pins the exact violation format.
func TestTodo_GOV_014_Golden(t *testing.T) {
	v := CheckPlanningContradictions("All five reference workflows are in scope for the pilot release.")
	if len(v) != 1 {
		t.Fatalf("expected exactly one violation, got %v", v)
	}
	const want = `line 1: all reference workflows treated as product scope: "All five reference workflows are in scope"`
	if got := v[0].String(); got != want {
		t.Errorf("violation message changed:\n got:  %s\n want: %s", got, want)
	}
}
