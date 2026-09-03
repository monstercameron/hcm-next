package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ignoredDirNames mirrors scripts/check-go-style.mjs's own ignore list,
// extended with "testdata" (Go's own package-discovery convention) so the
// quality gate never permanently flags a deliberately-bad fixture package
// that go build/vet/staticcheck already skip via "./...".
var ignoredDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"tmp":          true,
	"vendor":       true,
	"testdata":     true,
	"src":          true, // legacy module hcm-next-executor: a separate go.mod
}

// findGoFiles walks root, excluding ignoredDirNames, and returns every .go
// file it finds (relative to root, forward-slash separated).
func findGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && ignoredDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// gofmtViolations runs `gofmt -l` over every .go file findGoFiles reports
// under root and returns the ones with formatting drift (paths relative to
// root).
func gofmtViolations(root string) ([]string, error) {
	files, err := findGoFiles(root)
	if err != nil {
		return nil, fmt.Errorf("finding Go files: %w", err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Windows caps a command line at 32 KB and the tree holds more Go files
	// than fit in one argv, so gofmt runs over bounded chunks.
	const chunkSize = 200
	var stdout bytes.Buffer
	for start := 0; start < len(files); start += chunkSize {
		end := min(start+chunkSize, len(files))
		args := append([]string{"-l"}, files[start:end]...)
		cmd := exec.Command("gofmt", args...)
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("gofmt -l failed: %w\n%s", err, stderr.String())
		}
	}

	var violations []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			violations = append(violations, filepath.ToSlash(line))
		}
	}
	return violations, nil
}

// runGoVet runs `go vet <pattern>` from root and returns its combined
// output plus whether it succeeded.
func runGoVet(root, pattern string) (string, bool) {
	cmd := exec.Command("go", "vet", pattern)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// runStaticcheck runs `go tool staticcheck <pattern>` from root and returns
// its combined output plus whether it succeeded.
func runStaticcheck(root, pattern string) (string, bool) {
	cmd := exec.Command("go", "tool", "staticcheck", pattern)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}
