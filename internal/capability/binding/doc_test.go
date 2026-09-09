package binding

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestDoc_PackageBuildsALiveTable is the smoke test for the package doc's
// central claim: Build answers over the real registry and the real
// descriptors without any input from the caller beyond an optional handler
// index.
func TestDoc_PackageBuildsALiveTable(t *testing.T) {
	table, err := Build(nil)
	if err != nil {
		t.Fatalf("Build(nil): %v", err)
	}
	if table.SymbolsChecked {
		t.Errorf("Build(nil) reported SymbolsChecked=true; with no index nothing was verified")
	}
	if len(table.Entries)+len(table.Gaps) == 0 {
		t.Fatal("Build produced neither entries nor gaps; the join did not run")
	}
}

// TestDoc_PackageStaysKernelPure guards the purity claim in the package
// doc: no non-test file in this package may reach the filesystem, the
// clock, the network or the transport layer. The check is textual on
// purpose — it is the import list a reviewer would read.
func TestDoc_PackageStaysKernelPure(t *testing.T) {
	forbidden := []string{
		"\"os\"",
		"\"net\"",
		"\"net/http\"",
		"\"time\"",
		"path/filepath",
		"human-capital-management-suite/internal/transport",
		"google.golang.org/grpc",
		"google.golang.org/protobuf",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			t.Fatalf("reading %s: %v", name, readErr)
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, name, data, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		for _, imp := range file.Imports {
			for _, bad := range forbidden {
				if imp.Path.Value == bad || strings.Contains(imp.Path.Value, strings.Trim(bad, `"`)) {
					t.Errorf("%s imports %s; this package is kernel-pure and must not (see doc.go)", name, imp.Path.Value)
				}
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no non-test Go file was checked; the purity guard is vacuous")
	}
}
