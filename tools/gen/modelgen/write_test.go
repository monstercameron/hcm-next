package modelgen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoRootFindsGoMod(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := RepoRoot(wd)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved root %s has no go.mod: %v", root, err)
	}
}

func TestRepoRootNoGoMod(t *testing.T) {
	dir := t.TempDir()
	if _, err := RepoRoot(dir); err == nil {
		t.Fatal("RepoRoot succeeded from an isolated temp dir with no go.mod above it; want an error")
	}
}

func TestWriteAllWritesFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		OutputFile: []byte("package model\n"),
	}
	if err := WriteAll(dir, files); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(OutputDir), OutputFile))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != "package model\n" {
		t.Fatalf("written content = %q", got)
	}
}

func TestSortedFileKeys(t *testing.T) {
	files := map[string][]byte{"b.go": nil, "a.go": nil, "c.go": nil}
	got := sortedFileKeys(files)
	want := []string{"a.go", "b.go", "c.go"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedFileKeys = %v, want %v", got, want)
		}
	}
}
