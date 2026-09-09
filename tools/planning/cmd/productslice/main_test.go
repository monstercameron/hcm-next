package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/productslice"
)

func TestRunWritesValidatedRegistry(t *testing.T) {
	root, err := productslice.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "product-slices.yaml")
	var stdout, stderr bytes.Buffer

	if err := run([]string{"-root", root, "-out", outPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run() = %v, stderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}

	registry, err := productslice.LoadRegistryYAML(outPath)
	if err != nil {
		t.Fatalf("LoadRegistryYAML(%s): %v", outPath, err)
	}
	if err := registry.VerifyDigest(); err != nil {
		t.Errorf("generated file has a stale digest: %v", err)
	}
	if len(registry.Slices) != 1 || registry.Slices[0].SliceID != "promotion" {
		t.Fatalf("generated registry = %+v, want exactly one promotion slice", registry.Slices)
	}

	if !bytes.Contains(data, []byte("DO NOT EDIT")) {
		t.Error("generated file is missing the generated-file header")
	}
	if stdout.Len() == 0 {
		t.Error("run() printed nothing to stdout")
	}
}

func TestRunMatchesGeneratorPackageOutputExactly(t *testing.T) {
	root, err := productslice.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "product-slices.yaml")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", root, "-out", outPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run() = %v, stderr: %s", err, stderr.String())
	}

	written, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}

	fresh, err := productslice.RenderRegistryFile(productslice.NewRegistry(productslice.PromotionSliceDefinition()))
	if err != nil {
		t.Fatalf("RenderRegistryFile: %v", err)
	}

	if string(written) != string(fresh) {
		t.Error("run()'s output does not match productslice.RenderRegistryFile for the same definition")
	}
}

func TestRunRejectsInvalidFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-unknown-flag"}, &stdout, &stderr); err == nil {
		t.Fatal("expected an error for an unrecognized flag")
	}
}
