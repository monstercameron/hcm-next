package oraclestrength

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleStrengthRejectsExecutionOnlyAssertions(t *testing.T) {
	report, err := Scan(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]TestOracle, len(report.Tests))
	for _, oracle := range report.Tests {
		byName[oracle.Name] = oracle
	}
	weak, ok := byName["TestExecutionOnly"]
	if !ok || weak.Strength != WeakOracle {
		t.Fatalf("execution-only oracle = %+v, want WEAK_ORACLE", weak)
	}
	if len(report.Findings) != 1 || report.Findings[0].TestName != "TestExecutionOnly" {
		t.Fatalf("findings = %+v, want exactly the weak fixture test", report.Findings)
	}
	strong, ok := byName["TestTodo_GOV_021_Mutation"]
	if !ok || strong.Strength != StrongOracle {
		t.Fatalf("exact error oracle = %+v, want STRONG_ORACLE", strong)
	}
}

func TestTodo_GOV_021_Property(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Strength
	}{
		{name: "exact result", body: `func TestExact(t *testing.T) { if got, want := Run(); got != want { t.Fatal(got) } }`, want: StrongOracle},
		{name: "status only", body: `func TestStatus(t *testing.T) { if got := call(); got != 200 { t.Fatal(got) } }`, want: WeakOracle},
		{name: "alternatives", body: `func TestAlternatives(t *testing.T) { if got := Run(); got == "a" || got == "b" { t.Fatal(got) } }`, want: WeakOracle},
		{name: "clock", body: `func TestClock(t *testing.T) { if time.Now().IsZero() { t.Fatal("now") } }`, want: WeakOracle},
		{name: "random", body: `func TestRandom(t *testing.T) { if rand.Int() == 0 { t.Fatal("random") } }`, want: WeakOracle},
		{name: "map golden", body: `func TestMapGolden(t *testing.T) { values := map[string]int{"a": 1}; for key, value := range values { Golden(key, value) } }`, want: WeakOracle},
		{name: "mock", body: `func TestMock(t *testing.T) { mock.AssertCalled(t, "Save") }`, want: WeakOracle},
		{name: "coverage", body: `func TestCoverage(t *testing.T) { if testing.Coverage() < 1 { t.Fatal("coverage") } }`, want: WeakOracle},
		{name: "bare execution", body: `func TestBareExecution(t *testing.T) { len([]string{"side effect"}) }`, want: WeakOracle},
		{name: "prohibited", body: `func TestProhibited(t *testing.T) { if err != ErrDenied { t.Fatal(err) } }`, want: StrongOracle},
		{name: "effect count", body: `func TestEffectCount(t *testing.T) { if len(events) != 1 { t.Fatal(events) } }`, want: StrongOracle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "sample_test.go")
			source := "package sample\nimport \"testing\"\nfunc Run() string { return \"a\" }\nfunc call() int { return 200 }\n" + tc.body + "\n"
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			report, err := Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Tests) != 1 || report.Tests[0].Strength != tc.want {
				t.Fatalf("oracle = %+v, want %s", report.Tests, tc.want)
			}
		})
	}
}

func TestTodo_GOV_021_AdversarialOracleBoundaries(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Strength
	}{
		{name: "range conjunction is exact", body: `func TestRange(t *testing.T) { if count > 0 && count < 10 { t.Fatal(count) } }`, want: StrongOracle},
		{name: "equal conjunction is contradictory", body: `func TestContradictory(t *testing.T) { if got == "a" && got == "b" { t.Fatal(got) } }`, want: WeakOracle},
		{name: "repeated equality is satisfiable", body: `func TestRepeated(t *testing.T) { if got == "a" && got == "a" { t.Fatal(got) } }`, want: StrongOracle},
		{name: "equality and different inequality is satisfiable", body: `func TestMixed(t *testing.T) { if got == "a" && got != "b" { t.Fatal(got) } }`, want: StrongOracle},
		{name: "reversed equality and negation is contradictory", body: `func TestReversed(t *testing.T) { if "a" == got && got != "a" { t.Fatal(got) } }`, want: WeakOracle},
		{name: "direct map golden is unstable", body: `func TestDirectMapGolden(t *testing.T) { values := map[string]int{"a": 1}; Golden(values) }`, want: WeakOracle},
		{name: "map alias snapshot is unstable", body: `func TestMapAliasSnapshot(t *testing.T) { values := map[string]int{"a": 1}; snapshot := values; MatchSnapshot(snapshot) }`, want: WeakOracle},
		{name: "typed map snapshot is unstable", body: `func TestTypedMapSnapshot(t *testing.T) { var values map[string]int; MatchSnapshot(values) }`, want: WeakOracle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "sample_test.go")
			source := "package sample\nimport \"testing\"\nvar count int\n" + tc.body + "\n"
			if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			report, err := Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Tests) != 1 || report.Tests[0].Strength != tc.want {
				t.Fatalf("oracle = %+v, want %s", report.Tests, tc.want)
			}
		})
	}
}

func TestTodo_GOV_021_MapNamesAreFunctionScoped(t *testing.T) {
	root := t.TempDir()
	source := `package sample
import "testing"
func TestMap(t *testing.T) { got := map[string]int{"a": 1}; MatchSnapshot(got) }
func TestScalar(t *testing.T) { got := "stable"; Golden(got); if got != "stable" { t.Fatal(got) } }
`
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tests) != 2 || report.Tests[0].Strength != WeakOracle || report.Tests[1].Strength != StrongOracle {
		t.Fatalf("oracles = %+v, want map weak and sibling scalar strong", report.Tests)
	}
}

func TestTodo_GOV_021_PackageMapInference(t *testing.T) {
	root := t.TempDir()
	source := `package sample
import "testing"
var source = map[string]int{"a": 1}
var alias = source
func TestPackageMap(t *testing.T) { Golden(alias); if len(alias) != 1 { t.Fatal(alias) } }
`
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tests) != 1 || report.Tests[0].Strength != WeakOracle {
		t.Fatalf("oracles = %+v, want inferred package map weak", report.Tests)
	}
}

func TestTodo_GOV_021_Golden(t *testing.T) {
	report, err := Scan(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := GoldenJSON(report)
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

func TestTodo_GOV_021_Mutation(t *testing.T) {
	check, err := CheckMutation(context.Background(), filepath.Join("testdata", "fixture"), "TestTodo_GOV_021_Mutation")
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Report.Mutants) != 2 || len(check.Report.Survived) != 0 || len(check.Report.Killed) != 2 {
		t.Fatalf("mutation check = %+v, want two killed comparison mutants", check)
	}
}

func TestRegistryCheckResolvesStrongNamesAndReportsWeakNames(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "fixture")
	if err := copyFixture(filepath.Join("testdata", "fixture"), fixture); err != nil {
		t.Fatal(err)
	}
	registryDir := filepath.Join(root, "definitions", "planning")
	if err := os.MkdirAll(registryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := []map[string]any{
		{"id": "GOV-021", "test": "TestTodo_GOV_021_Mutation", "test_matrix": map[string]string{"PRIMARY": "TestTodo_GOV_021_Mutation", "PROPERTY": "TestExecutionOnly"}},
	}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(registryDir, "todo-registry.json")
	if err := os.WriteFile(registryPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	allowlistPath := filepath.Join(root, "allowlist.json")
	if err := os.WriteFile(allowlistPath, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := CheckRegistry(root, registryPath, allowlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != "WEAK_REGISTRY_ORACLE" || report.Findings[0].Class != "PROPERTY" {
		t.Fatalf("registry findings = %+v, want one weak PROPERTY finding", report.Findings)
	}
	if err := os.WriteFile(allowlistPath, []byte(`[{"todo_id":"GOV-021","class":"PROPERTY","test_name":"TestExecutionOnly","owner":"reviewed-owner","reason":"fixture exception"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	accepted, err := CheckRegistry(root, registryPath, allowlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted.Findings) != 0 || len(accepted.Allowlisted) != 1 {
		t.Fatalf("allowlisted registry report = %+v, want one accepted exception", accepted)
	}
}

func copyFixture(from, to string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, entry.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
