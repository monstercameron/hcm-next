package productslice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoRootFindsGoMod(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod not found under detected root %s: %v", root, err)
	}
	if info.IsDir() {
		t.Fatalf("%s/go.mod is a directory, not a file", root)
	}
}

func TestRepoRootMatchesModulePath(t *testing.T) {
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if want := "module github.com/monstercameron/human-capital-management-suite"; !strings.Contains(string(data), want) {
		t.Fatalf("go.mod at detected root does not declare %q", want)
	}
}
