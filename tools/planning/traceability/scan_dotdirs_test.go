package traceability

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanTestNamesSkipsDotDirectories(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pkg/real_test.go", "package pkg\n\nfunc TestReal(t *testing.T) {}\n")
	write(".artifacts/tmp/go-build1/b001/ghost_test.go", "package ghost\n\nfunc TestGhost(t *testing.T) {}\n")
	write("node_modules/dep/dep_test.go", "package dep\n\nfunc TestDep(t *testing.T) {}\n")
	write("pkg/testdata/fixture_test.go", "package fixture\n\nfunc TestFixture(t *testing.T) {}\n")

	names, err := ScanTestNames(root)
	if err != nil {
		t.Fatalf("ScanTestNames: %v", err)
	}
	if !names["TestReal"] {
		t.Fatal("the real test was not found")
	}
	for _, ghost := range []string{"TestGhost", "TestDep", "TestFixture"} {
		if names[ghost] {
			t.Errorf("%s came from a directory the scan must skip", ghost)
		}
	}
}
