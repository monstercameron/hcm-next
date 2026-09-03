// Package repopath locates the root module's directory and module path from
// within any tools/policy test, regardless of the working directory `go
// test` starts each package in. It is a test-support helper only: nothing
// under tools/policy ships in a release image.
package repopath

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/mod/modfile"
)

// RootDir returns the absolute path of the directory containing the root
// module's go.mod, walking up from this source file's own location. It
// panics on failure because every caller is a test that cannot proceed
// without it.
func RootDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("repopath: runtime.Caller failed to report this file's path")
	}

	dir := filepath.Dir(thisFile)
	for {
		candidate := filepath.Join(dir, "go.mod")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			panic(fmt.Sprintf("repopath: no go.mod found walking up from %s", thisFile))
		}
		dir = parent
	}
}

// ModulePath parses the "module" directive out of root's go.mod.
func ModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		panic(fmt.Sprintf("repopath: reading go.mod: %v", err))
	}

	modulePath := modfile.ModulePath(data)
	if modulePath == "" {
		panic("repopath: go.mod has no module directive")
	}
	return modulePath
}
