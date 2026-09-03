package drift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodo_MSRC_010(t *testing.T) {
	root := repoRoot(t)
	r := CheckRepository(root)
	if !r.Clean() {
		t.Fatal(r.Error())
	}
}

func TestTodo_MSRC_010_GoldenReportsOwnershipPath(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "doc.md")
	if err := os.WriteFile(p, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Check(Config{Root: repoRoot(t), GeneratedPath: "missing.go", Documents: []Document{{Path: p, Digest: "sha256:wrong"}}})
	if r.Clean() {
		t.Fatal("expected drift findings")
	}
	err := r.Error()
	if err == nil || !strings.Contains(err.Error(), "generated drift") || !strings.Contains(err.Error(), "document drift") {
		t.Fatalf("error lacks ownership paths: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("repository root not found")
		}
		dir = next
	}
}
