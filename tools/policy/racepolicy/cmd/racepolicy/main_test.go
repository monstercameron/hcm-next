package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/racepolicy"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestRun_PassWithModuleFlag(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

import "sync"

var mu sync.Mutex
`)
	writeFile(t, root, "pkga/a_test.go", `package pkga

import "testing"

func TestNothing(t *testing.T) {}
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-module", "example.com/mod"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Errorf("stdout = %q, want it to report PASS", stdout.String())
	}
}

func TestRun_FailReportsViolation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

import "sync"

var mu sync.Mutex
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-module", "example.com/mod"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "example.com/mod/pkga") {
		t.Errorf("stdout = %q, want it to name the violating package", stdout.String())
	}
}

// TestRun_ModuleFromGoMod proves the -module default: reading the module
// path out of -root/go.mod when the flag is omitted, using this
// repository's own go.mod as the fixture.
func TestRun_ModuleFromGoMod(t *testing.T) {
	root := repopath.RootDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 0 && code != 1 {
		t.Fatalf("run() = %d, want 0 or 1 (a real scan of the live repo); stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "concurrent package(s) declared") {
		t.Errorf("stdout = %q, want the declared-package count line", stdout.String())
	}
}

func TestRun_MissingGoModAndNoModuleFlag(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run() = %d, want 2 for a missing go.mod with no -module override", code)
	}
	if stderr.Len() == 0 {
		t.Error("expected a diagnostic on stderr")
	}
}

func TestRun_BadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-not-a-real-flag"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run() = %d, want 2 for an unrecognized flag", code)
	}
}

// TestRun_ListPrintsOnlyImportPaths pins the contract
// .github/workflows/tests.yml depends on: -list writes one import path per
// line and nothing else, so the workflow can pipe the output straight into
// `go test -race` without filtering. A stray human-readable line here would
// become an unknown package argument there.
func TestRun_ListPrintsOnlyImportPaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "concurrent/a.go", `package concurrent

import "sync"

var mu sync.Mutex
`)
	writeFile(t, root, "goroutine/b.go", `package goroutine

func Start() { go func() {}() }
`)
	// No concurrency primitive: must not appear in the list.
	writeFile(t, root, "plain/c.go", `package plain

const Answer = 42
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-module", "example.com/mod", "-list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(-list) = %d, want 0; stderr=%s", code, stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	want := []string{"example.com/mod/concurrent", "example.com/mod/goroutine"}
	if len(lines) != len(want) {
		t.Fatalf("-list printed %d line(s) %q, want exactly %q", len(lines), lines, want)
	}
	for i, line := range lines {
		if strings.TrimSpace(line) != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, line, want[i])
		}
	}
}

// TestRun_ListDoesNotEvaluateThePolicy proves -list is a pure listing: a
// package that violates the policy (concurrent, no test file) is still
// listed, and the exit code is still 0. The workflow keeps the policy check
// as its own separate step, so the race run is never silently narrowed by a
// listing failure.
func TestRun_ListDoesNotEvaluateThePolicy(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkga/a.go", `package pkga

import "sync"

var mu sync.Mutex
`)

	var listOut, listErr bytes.Buffer
	if code := run([]string{"-root", root, "-module", "example.com/mod", "-list"}, &listOut, &listErr); code != 0 {
		t.Fatalf("run(-list) = %d, want 0 even though the package violates the policy; stderr=%s", code, listErr.String())
	}
	if got := strings.TrimSpace(listOut.String()); got != "example.com/mod/pkga" {
		t.Errorf("-list stdout = %q, want the violating package listed anyway", got)
	}

	var gateOut, gateErr bytes.Buffer
	if code := run([]string{"-root", root, "-module", "example.com/mod"}, &gateOut, &gateErr); code != 1 {
		t.Errorf("run() without -list = %d, want 1: the same tree must still fail the policy", code)
	}
}

// TestRun_ListOverLiveRepositoryCoversTheRaceSet is the join between the two
// halves of TOOL-012 on this repository itself: every import path -list emits
// is a package Evaluate declared concurrent, and every concurrent package
// Evaluate declared is emitted. The workflow races exactly this set, so the
// policy step's verdict and the race step's coverage cannot drift apart.
func TestRun_ListOverLiveRepositoryCoversTheRaceSet(t *testing.T) {
	root := repopath.RootDir()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-root", root, "-list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(-list) = %d, want 0; stderr=%s", code, stderr.String())
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			listed[line] = true
		}
	}
	if len(listed) == 0 {
		t.Fatal("-list emitted nothing for the live repository")
	}

	report, err := racepolicy.Evaluate(root, moduleOf(t, root))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(report.Findings) != len(listed) {
		t.Fatalf("-list emitted %d package(s), Evaluate declared %d", len(listed), len(report.Findings))
	}
	for _, finding := range report.Findings {
		if !listed[finding.Package.ImportPath] {
			t.Errorf("Evaluate declared %s concurrent but -list did not emit it, so the race run would skip it", finding.Package.ImportPath)
		}
	}
}

func moduleOf(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	path := modfile.ModulePath(data)
	if path == "" {
		t.Fatal("go.mod has no module directive")
	}
	return path
}
