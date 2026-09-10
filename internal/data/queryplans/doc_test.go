package queryplans_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestPackageDocIsPresent holds doc.go's own existence to account: a package
// this small earns its documentation once, in one file, rather than everyone
// duplicating it per-file.
func TestPackageDocIsPresent(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	// parser.ParseDir is deprecated; the package doc lives in doc.go, so
	// parse that file directly (build-tag precision is irrelevant here).
	file, err := parser.ParseFile(fset, "doc.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse doc.go: %v", err)
	}
	if file.Name.Name != "queryplans" {
		t.Fatalf("doc.go declares package %q, want queryplans", file.Name.Name)
	}
	if file.Doc == nil || strings.TrimSpace(file.Doc.Text()) == "" {
		t.Fatal("package queryplans has no doc comment")
	}
}
