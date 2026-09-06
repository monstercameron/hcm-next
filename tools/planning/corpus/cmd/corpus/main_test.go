package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTodo_GOV_024_CommandReportsNewOrphans(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := repoRoot(t)
	code := run([]string{"-root", root, "-allowlist", filepath.Join(t.TempDir(), "missing-allowlist.json")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run exit code = %d, want new-orphan failure", code)
	}
	if stdout.Len() == 0 && stderr.Len() == 0 {
		t.Fatal("command produced no diagnostics")
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
