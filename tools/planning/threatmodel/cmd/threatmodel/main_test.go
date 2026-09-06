package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTodo_SECARCH_006_Integration(t *testing.T) {
	root := repoRoot(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr, func() time.Time {
		return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	})
	if code != 0 {
		t.Fatalf("run() = %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "threatmodel: PASS") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestTodo_SECARCH_006_Conformance(t *testing.T) {
	root := repoRoot(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-fixture", filepath.Join(root, "tools", "planning", "threatmodel", "testdata", "missing.yaml")}, &stdout, &stderr, time.Now)
	if code == 0 || !strings.Contains(stderr.String(), "read fixture") {
		t.Fatalf("missing fixture code=%d stderr=%q", code, stderr.String())
	}
}

func TestTodo_SECARCH_006_Security(t *testing.T) {
	root := repoRoot(t)
	fixture, err := os.ReadFile(filepath.Join(root, "tools", "planning", "threatmodel", "testdata", "threat-model.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fixture = bytes.Replace(fixture, []byte("\n  - name: trust\n"), []byte("\n  - name: missing\n"), 1)
	path := filepath.Join(t.TempDir(), "threat-model.yaml")
	if err := os.WriteFile(path, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root, "-fixture", path}, &stdout, &stderr, func() time.Time {
		return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	})
	if code == 0 || !strings.Contains(stderr.String(), "missing release-surface record") {
		t.Fatalf("missing surface code=%d stderr=%q", code, stderr.String())
	}
}

func TestTodo_SECARCH_006_Mutation(t *testing.T) {
	root := repoRoot(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", root}, &stdout, &stderr, func() time.Time {
		return time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	})
	if code == 0 || !strings.Contains(stderr.String(), "expires") {
		t.Fatalf("expired exception code=%d stderr=%q", code, stderr.String())
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
