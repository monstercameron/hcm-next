package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunCleanFixture(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "planning/plan.md", "# Plan\n\n[spec](specs/spec.md#contract)\n")
	writeFixture(t, root, "planning/specs/spec.md", "# Contract\n\n## Contract\n\nUses `FX-001`.\n")
	writeFixture(t, root, "registry.json", `[{"id":"FX-001"}]`)
	var out, errOut bytes.Buffer
	if code := run([]string{"-root", root, "-registry", "registry.json"}, &out, &errOut); code != 0 {
		t.Fatalf("run() = %d; stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "PASS") {
		t.Fatalf("stdout = %q, want PASS", out.String())
	}
}

func TestRunMissingRegistry(t *testing.T) {
	root := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"-root", root}, &out, &errOut); code != 2 {
		t.Fatalf("run() = %d, want 2", code)
	}
}

func TestRunReportsViolation(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "planning/plan.md", "# Plan\n\n[broken](missing.md#nope)\n")
	writeFixture(t, root, "registry.json", `[]`)
	var out, errOut bytes.Buffer
	if code := run([]string{"-root", root, "-registry", "registry.json"}, &out, &errOut); code != 1 {
		t.Fatalf("run() = %d, want 1; stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "MISSING_DOCUMENT") {
		t.Fatalf("stdout = %q, want MISSING_DOCUMENT", out.String())
	}
}
