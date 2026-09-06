package queryplans_test

import (
	"go/doc"
	"go/parser"
	"go/token"
	"testing"
)

// TestPackageDocIsPresent holds doc.go's own existence to account: a package
// this small earns its documentation once, in one file, rather than everyone
// duplicating it per-file.
func TestPackageDocIsPresent(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse .: %v", err)
	}
	pkg, ok := pkgs["queryplans"]
	if !ok {
		t.Fatal(`package "queryplans" not found in this directory`)
	}
	docPkg := doc.New(pkg, "./", 0)
	if docPkg.Doc == "" {
		t.Fatal("package queryplans has no doc comment")
	}
}
