// Package reporoot locates the repository root (the directory containing
// go.mod) from the current working directory. It lets `go run
// ./tools/conformance` and this package's tests resolve planning/ paths the
// same way whether invoked from the repository root or a subdirectory,
// mirroring the convention already used by tools/quality.
package reporoot

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNotFound is returned when no go.mod is found walking up from the
// starting directory.
var ErrNotFound = errors.New("reporoot: no go.mod found walking up from working directory")

// Find walks up from the current working directory until it finds a
// directory containing go.mod, and returns that directory.
func Find() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return FindFrom(dir)
}

// FindFrom walks up from start until it finds a directory containing
// go.mod, and returns that directory.
func FindFrom(start string) (string, error) {
	dir := start
	for {
		info, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}
