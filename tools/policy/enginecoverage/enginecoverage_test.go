package enginecoverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRoot(t *testing.T, importEngine bool) string {
	t.Helper()
	root := t.TempDir()
	engineDir := filepath.Join(root, "internal", "engines", "eligibility")
	consumerDir := filepath.Join(root, "internal", "intent", "definitions")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(consumerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	engine := "package eligibility\n\nfunc Version() int { return 1 }\nfunc Explain() string { return \"eligibility\" }\n"
	consumer := "package definitions\n"
	if importEngine {
		consumer = "package definitions\n\nimport _ \"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility\"\n"
	}
	if err := os.WriteFile(filepath.Join(engineDir, "eligibility.go"), []byte(engine), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(consumerDir, "bindings.go"), []byte(consumer), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func fixtureRow() Responsibility {
	return Responsibility{Intent: "hcmnext.people.change_manager", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines"}
}

// TestTodo_ENGINE_COVERAGE_001 proves a named reusable computation has one
// owner, a real ARCH-GO-009 engine package, and parser-proven consumer wiring.
func TestTodo_ENGINE_COVERAGE_001(t *testing.T) {
	report, err := Scan(fixtureRoot(t, true), Options{Responsibilities: []Responsibility{fixtureRow()}, ImportRoots: []string{"internal/intent/definitions"}})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() {
		t.Fatalf("clean engine coverage scan = %+v", report.Violations())
	}
}

func TestTodo_ENGINE_COVERAGE_001_Golden(t *testing.T) {
	if len(Responsibilities) == 0 || len(DefaultResponsibilities()) != len(Responsibilities) {
		t.Fatal("canonical responsibility table is empty or not defensively copied")
	}
	intents := map[string]bool{}
	computations := map[string]bool{}
	for _, row := range Responsibilities {
		intents[row.Intent] = true
		computations[row.Computation] = true
	}
	if len(intents) != 14 {
		t.Fatalf("intent denominator = %d, want fourteen drafted definitions", len(intents))
	}
	for _, want := range []string{"eligibility", "effective dating", "pay band", "population", "transformation", "snapshot", "schedule"} {
		if !computations[want] {
			t.Fatalf("computation %q is missing from responsibility table", want)
		}
	}
	a, err := Scan(fixtureRoot(t, true), Options{Responsibilities: []Responsibility{fixtureRow()}, ImportRoots: []string{"internal/intent/definitions"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Scan(fixtureRoot(t, true), Options{Responsibilities: []Responsibility{fixtureRow()}, ImportRoots: []string{"internal/intent/definitions"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest() != b.Digest() {
		t.Fatalf("equivalent reports have different digests: %s != %s", a.Digest(), b.Digest())
	}
}

func TestTodo_ENGINE_COVERAGE_001_Race(t *testing.T) {
	root := fixtureRoot(t, true)
	const workers = 4
	results := make(chan string, workers)
	for i := 0; i < workers; i++ {
		go func() {
			report, err := Scan(root, Options{Responsibilities: []Responsibility{fixtureRow()}, ImportRoots: []string{"internal/intent/definitions"}})
			if err != nil {
				results <- err.Error()
				return
			}
			results <- report.Digest()
		}()
	}
	want := ""
	for i := 0; i < workers; i++ {
		got := <-results
		if strings.Contains(got, "error") {
			t.Fatal(got)
		}
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("concurrent scans differ: %s != %s", got, want)
		}
	}
}

func TestTodo_ENGINE_COVERAGE_001_Conformance(t *testing.T) {
	row := fixtureRow()
	missing, err := Scan(fixtureRoot(t, false), Options{Responsibilities: []Responsibility{row}, Allowlist: []ImplicitEntry{}, ImportRoots: []string{"internal/intent/definitions"}})
	if err != nil {
		t.Fatal(err)
	}
	if missing.OK() || len(missing.Violations()) != 1 || missing.Violations()[0].Kind != "IMPLICIT" {
		t.Fatalf("missing import report = %+v, want one IMPLICIT gap", missing)
	}
	allowlisted, err := Scan(fixtureRoot(t, false), Options{
		Responsibilities: []Responsibility{row}, ImportRoots: []string{"internal/intent/definitions"},
		Allowlist: []ImplicitEntry{{Intent: row.Intent, Computation: row.Computation, Package: row.Package, Owner: "shared-engines", Reason: "consumer wiring is scheduled"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !allowlisted.OK() || !allowlisted.Findings[0].Allowlisted {
		t.Fatalf("reviewed implicit row was not allowlisted: %+v", allowlisted)
	}
}

func TestTodo_ENGINE_COVERAGE_001_Mutation(t *testing.T) {
	root := fixtureRoot(t, true)
	row := fixtureRow()
	row.Package = "internal/engines/missing"
	report, err := Scan(root, Options{Responsibilities: []Responsibility{row}, ImportRoots: []string{"internal/intent/definitions"}})
	if err != nil {
		t.Fatal(err)
	}
	var sawMissing bool
	for _, finding := range report.Violations() {
		if finding.Kind == "MISSING_PACKAGE" {
			sawMissing = true
		}
	}
	if !sawMissing {
		t.Fatalf("renamed engine package was accepted: %+v", report)
	}
}
