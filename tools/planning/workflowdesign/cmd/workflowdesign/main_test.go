package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowDesignCommand(t *testing.T) {
	seeds := filepath.Join("..", "..", "testdata", "seed", "records.yaml")
	dir := t.TempDir()
	outGo := filepath.Join(dir, "design_registry.go")
	outProto := filepath.Join(dir, "design_registry.proto")
	outGolden := filepath.Join(dir, "golden.json")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-seeds", seeds, "-out-go", outGo, "-out-proto", outProto, "-out-golden", outGolden}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	for _, path := range []string{outGo, outProto, outGolden} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Fatalf("%s is empty", path)
		}
	}
	if code := run([]string{}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing flags exit = %d, want 2", code)
	}
	if code := run([]string{"-seeds", filepath.Join(dir, "missing.yaml"), "-out-go", outGo, "-out-proto", outProto, "-out-golden", outGolden}, &stdout, &stderr); code != 1 {
		t.Fatalf("missing seeds exit = %d, want 1", code)
	}
}

func TestWorkflowDesignCommandFlagError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad flag exit = %d, want 2", code)
	}
}

func TestWorkflowDesignCommandRejectsInvalidRecords(t *testing.T) {
	dir := t.TempDir()
	seeds := filepath.Join(dir, "records.yaml")
	if err := os.WriteFile(seeds, []byte("records:\n  - intent: Broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"-seeds", seeds, "-out-go", filepath.Join(dir, "out.go"), "-out-proto", filepath.Join(dir, "out.proto"), "-out-golden", filepath.Join(dir, "golden.json")}
	if code := run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid records exit = %d, want 1", code)
	}
}

func TestWorkflowDesignCommandWriteFailures(t *testing.T) {
	seeds := filepath.Join("..", "..", "testdata", "seed", "records.yaml")
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-dir")
	cases := map[string][]string{
		"go":     {"-seeds", seeds, "-out-go", filepath.Join(missing, "out.go"), "-out-proto", filepath.Join(dir, "out.proto"), "-out-golden", filepath.Join(dir, "golden.json")},
		"proto":  {"-seeds", seeds, "-out-go", filepath.Join(dir, "out.go"), "-out-proto", filepath.Join(missing, "out.proto"), "-out-golden", filepath.Join(dir, "golden.json")},
		"golden": {"-seeds", seeds, "-out-go", filepath.Join(dir, "out.go"), "-out-proto", filepath.Join(dir, "out.proto"), "-out-golden", filepath.Join(missing, "golden.json")},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 1 {
				t.Fatalf("write failure exit = %d, want 1", code)
			}
		})
	}
}
