package extract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_PERFOPT_003_Golden(t *testing.T) {
	root := repoRoot(t)
	files, err := Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	h := sha256.New()
	for _, file := range files {
		h.Write([]byte(file.RelPath))
		h.Write([]byte{0})
		h.Write(file.Contents)
		h.Write([]byte{0})
	}
	got := hex.EncodeToString(h.Sum(nil)) + "\n"

	goldenPath := filepath.Join(root, "internal", "governance", "legal", "extract", "testdata", "perfopt_003_extraction.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to update)", goldenPath, err)
	}
	if !bytes.Equal(want, []byte(got)) {
		t.Fatalf("extraction digest changed: got %s, want %s", got, want)
	}
}

func BenchmarkTodo_PERFOPT_003(b *testing.B) {
	root, err := legal.RepoRoot()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Generate(root); err != nil {
			b.Fatal(err)
		}
	}
}
