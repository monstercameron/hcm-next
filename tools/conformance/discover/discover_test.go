package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/internal/reporoot"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

func testVocab(t *testing.T) (*vocab.Vocabulary, string) {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	v, err := vocab.Load(filepath.Join(root, "planning", "specs", "workflow-runtime.md"))
	if err != nil {
		t.Fatalf("vocab.Load: %v", err)
	}
	return v, root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDocumentsSortedAndIsolatesPerFileErrors(t *testing.T) {
	v, root := testVocab(t)
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "zzz-valid.md"), "# ZZZ Valid\n\n## Required conformance scenarios\n\n1. Happy path.\n")
	writeFile(t, filepath.Join(dir, "aaa-empty.md"), "   \n")
	writeFile(t, filepath.Join(dir, "not-markdown.txt"), "# Should be ignored\n")

	results, err := Documents(root, dir, v)
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2 (the .txt file must be excluded): %+v", len(results), results)
	}

	// Deterministic name order: "aaa-empty.md" before "zzz-valid.md".
	if got, want := filepath.Base(results[0].RelPath), "aaa-empty.md"; got != want {
		t.Errorf("results[0] = %s, want %s", got, want)
	}
	if results[0].Err == nil {
		t.Error("aaa-empty.md: Err = nil, want a parse error (empty document)")
	}
	if results[0].Doc != nil {
		t.Error("aaa-empty.md: Doc != nil despite a parse error")
	}

	if got, want := filepath.Base(results[1].RelPath), "zzz-valid.md"; got != want {
		t.Errorf("results[1] = %s, want %s", got, want)
	}
	if results[1].Err != nil {
		t.Errorf("zzz-valid.md: Err = %v, want nil", results[1].Err)
	}
	if results[1].Doc == nil {
		t.Fatal("zzz-valid.md: Doc = nil, want a parsed document")
	}
	if len(results[1].Doc.Workflows) != 1 {
		t.Errorf("zzz-valid.md: len(Workflows) = %d, want 1", len(results[1].Doc.Workflows))
	}
}

func TestDocumentsRelPathIsRepoRootRelative(t *testing.T) {
	v, root := testVocab(t)
	dir := filepath.Join(root, "tools", "conformance", "testdata", "fixtures", "discover-relpath")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	writeFile(t, filepath.Join(dir, "sample.md"), "# Sample\n\n## Required conformance scenarios\n\n1. One.\n")

	results, err := Documents(root, dir, v)
	if err != nil {
		t.Fatalf("Documents: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	want := "tools/conformance/testdata/fixtures/discover-relpath/sample.md"
	if results[0].RelPath != want {
		t.Errorf("RelPath = %q, want %q", results[0].RelPath, want)
	}
}

func TestDocumentsMissingDirectory(t *testing.T) {
	v, root := testVocab(t)
	if _, err := Documents(root, filepath.Join(t.TempDir(), "does-not-exist"), v); err == nil {
		t.Fatal("Documents(missing dir): got nil error, want non-nil")
	}
}
