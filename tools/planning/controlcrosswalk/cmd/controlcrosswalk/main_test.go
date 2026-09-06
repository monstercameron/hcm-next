package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTodo_GOV_030_Conformance(t *testing.T) {
	root := repoRoot(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "controlcrosswalk: PASS") || !strings.Contains(stdout.String(), "digest=") {
		t.Fatalf("stdout = %q, want PASS and digest", stdout.String())
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve package path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))))
}
