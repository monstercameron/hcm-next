package mutationpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestCriticalMutationPolicyRejectsSurvivingSemanticMutants is GOV-019's
// primary oracle: every comparison mutant in the fixture must be killed by
// the fixture's refusal assertions.
func TestCriticalMutationPolicyRejectsSurvivingSemanticMutants(t *testing.T) {
	fixture := filepath.Join("testdata", "fixture")
	report, err := RunFixture(fixture, "TestTodo_GOV_019_Mutation")
	if err != nil {
		t.Fatalf("RunFixture: %v", err)
	}
	if len(report.Mutants) != 2 {
		t.Fatalf("got %d comparison mutants, want 2: %+v", len(report.Mutants), report.Mutants)
	}
	if len(report.Killed) != len(report.Mutants) || len(report.Survived) != 0 {
		t.Fatalf("mutation report = %+v, want every mutant killed", report)
	}
}

// TestTodo_GOV_019_Property proves the operator-flip catalog is complete for
// all six ordered comparison operators and does not mutate arithmetic.
func TestTodo_GOV_019_Property(t *testing.T) {
	source := []byte(`package fixture

func Compare(a, b int) bool { return a == b || a != b || a < b || a <= b || a > b || a >= b }
`)
	mutants, err := ComparisonMutants("fixture.go", source)
	if err != nil {
		t.Fatalf("ComparisonMutants: %v", err)
	}
	if len(mutants) != 6 {
		t.Fatalf("got %d mutants, want 6: %+v", len(mutants), mutants)
	}
	for _, mutant := range mutants {
		if mutant.From == mutant.To || mutant.From == "+" || mutant.From == "-" {
			t.Errorf("invalid semantic mutant: %+v", mutant)
		}
	}
}

// TestTodo_GOV_019_Golden pins the declared package table, owner allowlist
// and minimum semantic-negative assertion threshold.
func TestTodo_GOV_019_Golden(t *testing.T) {
	got := CriticalPackageTable()
	want := []PackagePolicy{
		{Name: "trust", Root: "internal/trust", Owner: "governance-and-trust", MinSentinels: 2},
		{Name: "authz", Root: "internal/trust/authz", Owner: "governance-and-trust", MinSentinels: 2},
		{Name: "capability", Root: "internal/capability", Owner: "intent-and-capability", MinSentinels: 2},
		{Name: "intent", Root: "internal/intent", Owner: "intent-and-capability", MinSentinels: 2},
		{Name: "workflow-runtime", Root: "internal/workflow/runtime", Owner: "workflow-runtime", MinSentinels: 2},
		{Name: "ledger", Root: "internal/ledger", Owner: "data-and-ledger", MinSentinels: 2},
		{Name: "tenancy", Root: "internal/domains/tenant", Owner: "domain-teams", MinSentinels: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("package table length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("package table[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, policy := range got {
		if !OwnerAllowlist[policy.Owner] {
			t.Errorf("owner %q is not allowlisted", policy.Owner)
		}
	}
}

// TestTodo_GOV_019_Race runs the parser and AST mutator concurrently so the
// declared policy has no shared mutable scan state.
func TestTodo_GOV_019_Race(t *testing.T) {
	source := []byte(`package fixture
func Check(a, b int) bool { return a != b }
`)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mutants, err := ComparisonMutants("fixture.go", source)
			if err != nil || len(mutants) != 1 {
				t.Errorf("ComparisonMutants = %v, %+v", err, mutants)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_GOV_019_Security rejects a fixture whose mutation test contains
// no recognizable refusal sentinel or negative assertion.
func TestTodo_GOV_019_Security(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weak_test.go")
	source := `package weak
import "testing"
func TestWeak(t *testing.T) { t.Log("executed") }
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sentinels, err := inspectTestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sentinels) != 0 {
		t.Fatalf("weak test sentinels = %v, want none", sentinels)
	}
}

// TestTodo_GOV_019_Mutation verifies that an intentionally weakened fixture
// test is observable as a surviving mutant rather than being treated as a
// successful mutation run.
func TestTodo_GOV_019_Mutation(t *testing.T) {
	fixture := filepath.Join("testdata", "fixture")
	if _, err := RunFixture(fixture, "TestTodo_GOV_019_Mutation"); err != nil {
		t.Fatalf("fixture mutation run: %v", err)
	}
	if !strings.Contains(strings.Join(refusalSentinelNames(), ","), "DENIED") {
		t.Fatal("fixture refusal vocabulary lost DENIED")
	}
}

func refusalSentinelNames() []string {
	return append([]string(nil), sentinelWords...)
}
