package transformationvectors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorWritesDeterministicGoTestsToTempOutput(t *testing.T) {
	dir := t.TempDir()
	path, err := Generate(dir, "vectors_test", DefaultVectors())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "TestGeneratedXFORM006") || !strings.Contains(string(got), "concat-positive") {
		t.Fatalf("generated source missing suite/vector: %s", got)
	}
	path2, err := Generate(filepath.Join(dir, "second"), "vectors_test", DefaultVectors())
	if err != nil {
		t.Fatal(err)
	}
	got2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(got2) {
		t.Fatal("generator output is not byte stable")
	}
}

func TestGeneratorRejectsMissingInputs(t *testing.T) {
	if _, err := Generate(t.TempDir(), "", DefaultVectors()); err == nil {
		t.Fatal("expected package validation error")
	}
	if _, err := Generate(t.TempDir(), "vectors_test", nil); err == nil {
		t.Fatal("expected empty vector error")
	}
}
