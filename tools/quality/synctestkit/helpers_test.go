package synctestkit_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRoot walks up from this test file's own location to find go.mod.
// tools/quality/quality_test.go documents why every package under
// tools/quality repeats this small search rather than sharing a helper
// package: it keeps each check runnable in isolation.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from %s", thisFile)
		}
		dir = parent
	}
}
