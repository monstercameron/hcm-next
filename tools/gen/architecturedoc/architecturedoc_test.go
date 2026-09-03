package architecturedoc

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func root(t testing.TB) string {
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Join(filepath.Dir(f), "..", "..", "..")
}

func TestRenderIsDeterministicAndSorted(t *testing.T) {
	s := Snapshot{Module: "example.test", Digest: "abc", Packages: []Package{{ImportPath: "example.test/z", Root: "z", Owner: "z-owner", Layer: "domains", Phase: "P1A"}, {ImportPath: "example.test/a", Root: "a", Owner: "a-owner", Layer: "kernel", Phase: "P1A"}}, Edges: []Edge{{Importer: "example.test/z", Imported: "example.test/a"}}, Processes: map[string][]string{"z": {"worker (initial)"}}}
	a, b := Render(s), Render(s)
	if !bytes.Equal(a, b) {
		t.Fatal("same snapshot rendered different bytes")
	}
	if strings.Index(string(a), "| `example.test/a`") > strings.Index(string(a), "| `example.test/z`") {
		t.Fatal("package table is not sorted")
	}
	if !strings.Contains(string(a), "Import-graph digest: `abc`") {
		t.Fatal("digest missing")
	}
}

func TestLoadAndWriteOnlyToExplicitTempPath(t *testing.T) {
	s, err := Load(root(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Module == "" || len(s.Packages) == 0 || s.Digest == "" {
		t.Fatalf("incomplete snapshot: module=%q packages=%d digest=%q", s.Module, len(s.Packages), s.Digest)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "docs", DefaultOutputName)
	if err := Write(root(t), path); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if err := Check(root(t), path); err != nil {
		t.Fatalf("fresh output failed check: %v", err)
	}
	b, _ := os.ReadFile(path)
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(root(t), path); err == nil {
		t.Fatal("checker accepted stale output")
	}
}

func TestDigestStable(t *testing.T) {
	if Digest([]byte("x")) != Digest([]byte("x")) {
		t.Fatal("digest changed")
	}
	if Digest([]byte("x")) == Digest([]byte("y")) {
		t.Fatal("digest collision in test")
	}
}
