package compositionroot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// listpkg mirrors the subset of `go list -json` output the checker needs:
// a package's import path and its source directory. It stays local to this
// package because tools/quality must not import tools/policy's internal
// packages (Go's internal-package rule), and the checker is deliberately
// self-contained so it can run before the application packages exist.
type listpkg struct {
	ImportPath string
	Dir        string
}

func modulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	return modfile.ModulePath(data)
}

// listPackages runs `go list -json ./...` from root and decodes the
// concatenated JSON object stream `go list` prints (one object per package,
// no enclosing array or separators).
func listPackages(root string) ([]listpkg, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = root

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list -json ./... failed: %w\nstderr:\n%s", err, stderr.String())
	}

	var packages []listpkg
	decoder := json.NewDecoder(&stdout)
	for decoder.More() {
		var pkg listpkg
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}
