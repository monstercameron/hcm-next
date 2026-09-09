// Package effectfulpkg is a compiler_test.go fixture only: it exists to be
// scanned by ScanImports, never built as part of the module (Go tooling
// skips any directory named "testdata"). Its import of a nonexistent path
// is deliberate - go/parser.ParseFile with ImportsOnly never resolves
// imports, so this file parses cleanly without that path needing to exist.
package effectfulpkg

import (
	_ "fmt"
	_ "github.com/monstercameron/human-capital-management-suite/internal/connectivity/writeadapters"
)
