package modelgen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// OutputDir is the generated package's location under the repository root,
// mirroring the protoc-gen-go output already committed beside it
// (gen/go/hcmnext/<domain>/v1).
const OutputDir = "gen/go/hcmnext/model"

// RepoRoot walks up from dir looking for go.mod, the same convention
// [github.com/monstercameron/hcm-next/tools/gen/storagemanifest.RepoRoot]
// uses, so this package's tests and its cmd/modelgen entry point behave the
// same regardless of the working directory they run from. It is a small,
// self-contained copy rather than an import of that package: the two
// generators are independent MSRC lanes, and neither should need the other
// to build.
func RepoRoot(dir string) (string, error) {
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("modelgen: no go.mod found walking up from %s", dir)
		}
		dir = parent
	}
}

// WriteAll writes every file in files (as returned by [Render]) under
// <repoRoot>/<OutputDir>. It is the one place this package touches disk for
// output; [Build] and [Render] are pure. Files are written in sorted name
// order so a filesystem walk of the result is itself deterministic.
func WriteAll(repoRoot string, files map[string][]byte) error {
	dir := filepath.Join(repoRoot, filepath.FromSlash(OutputDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("modelgen: mkdir %s: %w", dir, err)
	}
	for _, name := range sortedFileKeys(files) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return fmt.Errorf("modelgen: write %s: %w", path, err)
		}
	}
	return nil
}

// sortedFileKeys returns files's keys sorted, so [WriteAll] never depends on
// Go's randomized map iteration order.
func sortedFileKeys(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
