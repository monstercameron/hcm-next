package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRejectsRegistryWeakOracle(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "definitions", "planning"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools", "policy", "oraclestrength"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte("package sample\nimport \"testing\"\nfunc TestWeak(t *testing.T) { if true { t.Fatal(\"failed\") } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "definitions", "planning", "todo-registry.json"), []byte(`[{"id":"GOV-021","test":"TestWeak","test_matrix":{"PRIMARY":"TestWeak"}}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "policy", "oraclestrength", "allowlist.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-root", root}, &stdout, &stderr); code != 1 {
		t.Fatalf("Run code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("Run emitted no finding report")
	}
}

func TestRunRejectsMissingRegistry(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-root", t.TempDir()}, &stdout, &stderr); code == 0 || stderr.Len() == 0 {
		t.Fatalf("Run code=%d stdout=%q stderr=%q, want non-zero diagnostic", code, stdout.String(), stderr.String())
	}
}
