package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"
)

func TestObligationsCommandRun(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "requirements.json")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", root, "-out", out}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.Len() == 0 {
		t.Fatal("command produced no status output")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))))
}
