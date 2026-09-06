package reconcile_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedReconcileImports is the exact set this package's non-test source may
// import, beyond the Go standard library. It is the executable form of
// doc.go's claim that this package coordinates observation and comparison
// through ports and never mutates domain or provider state: every entry here
// is either a pure value/port package (effectgraph, observe's evidence
// vocabulary, lease's fence vocabulary) or this package's own declared
// database capability (dbport, runtimestate's Executor alias). Nothing that
// writes to a domain aggregate or talks to a concrete external provider is
// on this list, and a source file that imports one fails this test rather
// than silently widening the package's reach.
var allowedReconcileImports = map[string]bool{
	"context":       true,
	"crypto/sha256": true,
	"encoding/hex":  true,
	"encoding/json": true,
	"errors":        true,
	"fmt":           true,
	"sort":          true,
	"sync":          true,
	"time":          true,

	"github.com/google/uuid": true,

	"github.com/monstercameron/hcm-next/internal/connectivity/observe": true,
	"github.com/monstercameron/hcm-next/internal/data/dbport":          true,
	"github.com/monstercameron/hcm-next/internal/data/runtimestate":    true,
	"github.com/monstercameron/hcm-next/internal/effectgraph":          true,
	"github.com/monstercameron/hcm-next/internal/workflow/lease":       true,
}

// TestTodo_RECON_001_Conformance scans every non-test .go file in this
// package for imports outside the declared allowlist. It is the check
// doc.go promises: reconcile coordinates observation and comparison through
// ports and never reaches a domain aggregate or a concrete provider
// directly, including internal/connectivity/observe's own adapters (a real
// database-backed observation store) or any internal/domains package.
func TestTodo_RECON_001_Conformance(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		scanned++
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if allowedReconcileImports[importPath] {
				continue
			}
			t.Fatalf("%s imports %q, which is not on this package's declared allowlist; "+
				"reconcile coordinates observation and comparison through ports and must never "+
				"reach a domain aggregate or a concrete provider directly", name, importPath)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no package source files; the conformance check proved nothing")
	}
}
