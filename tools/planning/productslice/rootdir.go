package productslice

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// RepoRoot walks up from this source file to the repository root (the
// directory containing go.mod), independent of the caller's working
// directory. Both the generator command (tools/planning/cmd/productslice)
// and this package's own tests use it, so root discovery never depends on
// how `go run`/`go test` happened to be invoked -- the same pattern
// tools/policy/internal/repopath uses, reimplemented here because that
// package is internal to tools/policy and cannot be imported from
// tools/planning.
func RepoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("productslice: runtime.Caller failed to report this file's path")
	}
	dir := filepath.Dir(thisFile)
	for {
		candidate := filepath.Join(dir, "go.mod")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("productslice: no go.mod found walking up from %s", thisFile)
		}
		dir = parent
	}
}
