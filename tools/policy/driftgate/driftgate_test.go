package driftgate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTodo_MSRC_010(t *testing.T) {
	tree := t.TempDir()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
	sourcePath := filepath.Join(repoRoot, "gen", "go", "hcmnext", "model", "model_generated.go")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	generatedDir := filepath.Join(tree, "gen", "go", "hcmnext", "model")
	if err := os.MkdirAll(generatedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(generatedDir, "model_generated.go")
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compareGeneratedFiles(generatedDir, map[string][]byte{"model_generated.go": source}, "modelgen"); err != nil {
		t.Fatalf("clean generated file was reported as drift: %v", err)
	}
	if err := os.WriteFile(path, append([]byte("perturbed\n"), source...), 0o644); err != nil {
		t.Fatal(err)
	}
	err = compareGeneratedFiles(generatedDir, map[string][]byte{"model_generated.go": source}, "modelgen")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("perturbed temp-tree file error = %v, want exact ownership path", err)
	}
}

func TestTodo_MSRC_010_Golden(t *testing.T) {
	report := Report{Checks: []CheckResult{
		{Name: "modelgen", RegenerationCommand: "go run ./tools/gen/modelgen/cmd/modelgen", Passed: true},
	}}
	if report.Digest() == "" || report.Digest() != report.Digest() {
		t.Fatal("drift report digest is not stable")
	}
	if !report.OK() {
		t.Fatal("passing drift report is not OK")
	}
	failed := Report{Checks: append(report.Checks, CheckResult{Name: "docs", RegenerationCommand: "go run ./tools/policy/docintegrity/cmd/docintegrity", Detail: "planning/plan.md is stale"})}
	if failed.OK() {
		t.Fatal("failed drift report was reported as OK")
	}
	first, ok := failed.FirstFailure()
	if !ok || first.Name != "docs" {
		t.Fatalf("FirstFailure = %+v, %v; want docs", first, ok)
	}
}
