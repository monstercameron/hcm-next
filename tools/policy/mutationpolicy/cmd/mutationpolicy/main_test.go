package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunRejectsMissingRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-root", t.TempDir()}, &stdout, &stderr); code == 0 {
		t.Fatalf("Run returned %d, want non-zero for missing registry", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("Run emitted no diagnostic")
	}
}

func TestRunReportsPolicyGaps(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..")
	if code := Run([]string{"-root", root}, &stdout, &stderr); code == 0 {
		t.Fatalf("Run returned %d, want a gap-bearing repository to fail", code)
	}
	if stdout.Len() == 0 {
		t.Fatal("Run emitted no report")
	}
}
