package repopath

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Package mirrors the subset of `go list -json` output tools/policy needs:
// a package's own import path and its direct imports (both standard
// library/third-party and within-module).
type Package struct {
	ImportPath string
	Dir        string
	Imports    []string
	Deps       []string
}

// ListPackages runs `go list -json ./...` from root and decodes the
// concatenated JSON object stream `go list` prints (one object per
// package, no enclosing array or separators).
func ListPackages(root string) ([]Package, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = root

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list -json ./... failed: %w\nstderr:\n%s", err, stderr.String())
	}

	var packages []Package
	decoder := json.NewDecoder(&stdout)
	for decoder.More() {
		var pkg Package
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}
