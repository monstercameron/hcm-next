package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
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
