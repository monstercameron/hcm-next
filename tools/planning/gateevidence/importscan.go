package gateevidence

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ScanImports returns the sorted, de-duplicated set of every import path
// declared by any .go file (production or _test.go) directly inside pkgDir.
// It does not recurse into subdirectories and does not resolve transitive
// imports - NEXT-003's RED clause is "a test whose package imports write
// paths the manifest forbids", a direct-import check on the evidence
// package itself, not a full dependency-graph walk. It uses go/parser
// (ImportsOnly mode) so the import path need not actually resolve to a
// buildable package, which is what lets compiler_test.go exercise this with
// a fixture "forbidden" import that names no real package.
func ScanImports(pkgDir string) ([]string, error) {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", pkgDir, err)
	}

	seen := make(map[string]bool)
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(pkgDir, entry.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imp := range file.Imports {
			value, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			seen[value] = true
		}
	}

	imports := make([]string, 0, len(seen))
	for imp := range seen {
		imports = append(imports, imp)
	}
	sort.Strings(imports)
	return imports, nil
}

// AnyForbidden returns the first import in imports that has one of
// forbiddenPrefixes as a prefix, and true if one was found.
func AnyForbidden(imports, forbiddenPrefixes []string) (string, bool) {
	for _, imp := range imports {
		for _, prefix := range forbiddenPrefixes {
			if prefix != "" && strings.HasPrefix(imp, prefix) {
				return imp, true
			}
		}
	}
	return "", false
}
