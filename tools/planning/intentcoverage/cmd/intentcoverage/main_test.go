package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTodo_GOV_026_CommandReportsCounts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := repoRoot(t)
	code := run([]string{"-root", root, "-allowlist", filepath.Join(t.TempDir(), "missing-allowlist.json")}, &stdout, &stderr)
	if code != 0 && code != 1 {
		t.Fatalf("run exit code = %d, want 0 (clean) or 1 (new orphans)", code)
	}
	if stdout.Len() == 0 {
		t.Fatal("command produced no diagnostics")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("intentcoverage: baseline=14")) {
		t.Fatalf("stdout missing expected baseline count summary line: %s", stdout.String())
	}
}

func TestTodo_GOV_026_CommandUpdateAllowlistWritesBaseline(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := repoRoot(t)
	allowPath := filepath.Join(t.TempDir(), "allowlist.json")
	code := run([]string{"-root", root, "-allowlist", allowPath, "-update-allowlist"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update-allowlist exit code = %d, stderr=%s", code, stderr.String())
	}

	var stdout2, stderr2 bytes.Buffer
	code = run([]string{"-root", root, "-allowlist", allowPath}, &stdout2, &stderr2)
	if code != 0 {
		t.Fatalf("run after update-allowlist exit code = %d, want 0 (PASS); stdout=%s stderr=%s", code, stdout2.String(), stderr2.String())
	}
	if !bytes.Contains(stdout2.Bytes(), []byte("intentcoverage: PASS")) {
		t.Fatalf("expected PASS after allowlist baseline; stdout=%s", stdout2.String())
	}
}

func TestTodo_GOV_026_CommandRejectsBadRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", filepath.Join(t.TempDir(), "does-not-exist")}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run exit code = %d, want 2 for a missing repository root", code)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve command test path")
	}
	root := filepath.Dir(file)
	for i := 0; i < 5; i++ {
		root = filepath.Dir(root)
	}
	return root
}
