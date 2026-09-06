package scheduler

import (
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestPackageDocumentsItsContract keeps the package comment honest about the
// properties SVC-004's RED clause names, so a later edit that quietly drops
// one of them from the documentation is a failing test rather than an
// unnoticed regression in the explanation callers read.
func TestPackageDocumentsItsContract(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "doc.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse doc.go: %v", err)
	}
	if file.Doc == nil {
		t.Fatal("doc.go carries no package comment")
	}
	doc := file.Doc.Text()
	for _, phrase := range []string{
		"SKIP LOCKED",
		"compare-and-swap",
		"WORKFLOW_INSTANCE",
		"restart loses nothing",
		"No workflow semantics",
		"github.com/google/uuid",
	} {
		if !strings.Contains(doc, phrase) {
			t.Errorf("package comment no longer explains %q", phrase)
		}
	}
}

// TestPackageNamesNoIdentifierModule is the placement rule this package is
// built around: definitions/architecture/dependency-roles.yaml does not list
// internal/platform among the roots allowed to import github.com/google/uuid,
// and tools/policy/libfirewall enforces that over the real import graph. This
// test fails here, in the owning package, rather than only in the policy
// suite, so the reason sits next to the code.
func TestPackageNamesNoIdentifierModule(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("parsed no package source at all")
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				if strings.Trim(imp.Path.Value, `"`) == "github.com/google/uuid" {
					t.Errorf("%s imports github.com/google/uuid; internal/platform is not one of its allowed roots", name)
				}
			}
		}
	}
}
