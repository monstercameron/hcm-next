package archrules

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ExportedTopLevelNames parses every non-test .go file directly inside dir
// (not recursive) and returns the set of exported top-level identifiers:
// function/method names (a method is recorded as "Type.Method"), type
// names, and var/const names. It is a syntax-only scan (no type-checking),
// sufficient for ARCH-GO-009's "does this package export a symbol named
// X" contract-completeness question.
func ExportedTopLevelNames(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("archrules: reading %s: %w", dir, err)
	}

	names := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("archrules: reading %s: %w", entry.Name(), err)
		}
		found, err := exportedTopLevelNamesFromSource(entry.Name(), string(data))
		if err != nil {
			return nil, err
		}
		for n := range found {
			names[n] = true
		}
	}
	return names, nil
}

// ExportedTopLevelNamesFromSource runs the same scan over in-memory source,
// for synthetic RED/GREEN engine-contract fixtures.
func ExportedTopLevelNamesFromSource(filename, source string) (map[string]bool, error) {
	return exportedTopLevelNamesFromSource(filename, source)
}

func exportedTopLevelNamesFromSource(filename, source string) (map[string]bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, 0)
	if err != nil {
		return nil, fmt.Errorf("archrules: parsing %s: %w", filename, err)
	}

	names := map[string]bool{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				names[receiverTypeName(d.Recv.List[0].Type)+"."+d.Name.Name] = true
				continue
			}
			names[d.Name.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						names[s.Name.Name] = true
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() {
							names[n.Name] = true
						}
					}
				}
			}
		}
	}
	return names, nil
}

func receiverTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

// ExportedInterfaces parses every non-test .go file directly inside dir and
// returns the names of every exported top-level `type X interface{...}`
// declaration. ARCH-GO-013 uses this to prove an adapter-marked package
// declares no port of its own (an adapter implements an interface owned
// elsewhere; it does not get to author the interface too).
func ExportedInterfaces(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("archrules: reading %s: %w", dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("archrules: reading %s: %w", entry.Name(), err)
		}
		found, err := exportedInterfacesFromSource(entry.Name(), string(data))
		if err != nil {
			return nil, err
		}
		names = append(names, found...)
	}
	return names, nil
}

// ExportedInterfacesFromSource runs the same scan over in-memory source.
func ExportedInterfacesFromSource(filename, source string) ([]string, error) {
	return exportedInterfacesFromSource(filename, source)
}

func exportedInterfacesFromSource(filename, source string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, 0)
	if err != nil {
		return nil, fmt.Errorf("archrules: parsing %s: %w", filename, err)
	}

	var names []string
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !ts.Name.IsExported() {
				continue
			}
			if _, ok := ts.Type.(*ast.InterfaceType); ok {
				names = append(names, ts.Name.Name)
			}
		}
	}
	return names, nil
}
