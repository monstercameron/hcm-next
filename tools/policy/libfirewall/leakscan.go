package libfirewall

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Exposure names one exported declaration, inside a package that is
// otherwise allowed to import a firewalled module, whose own type
// expression still names that module's package directly (a bare type
// alias, or an exported func/field/interface-method signature). Even a
// package inside allowed_import_roots must keep the third-party type
// behind an owned wrapper; a caller of that package should never receive a
// concrete third-party value without importing the third-party package by
// name itself.
type Exposure struct {
	File       string
	Line       int
	Name       string // the exposing declaration's name (Type, Type.Field, Func, ...)
	Kind       string // "alias" | "type" | "field" | "method" | "func"
	Type       string // printed type expression
	ImportPath string // the firewalled import path referenced
}

// ScanExposures parses every non-test .go file directly inside dir (not
// recursive - callers scan one package directory at a time) and reports
// every exported declaration whose type expression names one of
// firewalledImportPaths.
//
// This is a syntax-only scan: it recognizes a reference only through the
// scanned file's own import of the firewalled path. It cannot see a leak
// that travels through an intermediate alias defined in a different
// package two hops away; CheckImport's import-edge boundary is what bounds
// that case, by keeping the intermediate package itself out of
// unauthorized roots.
func ScanExposures(dir string, firewalledImportPaths []string) ([]Exposure, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("libfirewall: reading %s: %w", dir, err)
	}

	var exposures []Exposure
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("libfirewall: reading %s: %w", entry.Name(), err)
		}
		found, err := ScanSource(entry.Name(), string(data), firewalledImportPaths)
		if err != nil {
			return nil, err
		}
		exposures = append(exposures, found...)
	}
	sort.Slice(exposures, func(i, j int) bool {
		if exposures[i].File != exposures[j].File {
			return exposures[i].File < exposures[j].File
		}
		return exposures[i].Line < exposures[j].Line
	})
	return exposures, nil
}

// ScanSource runs the same scan directly over in-memory source, so RED
// fixtures can inject a synthetic leak without writing real files to disk.
func ScanSource(filename, source string, firewalledImportPaths []string) ([]Exposure, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("libfirewall: parsing %s: %w", filename, err)
	}

	aliasToImport := map[string]string{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !matchesAnyPrefix(path, firewalledImportPaths) {
			continue
		}
		local := importLocalName(imp, path)
		if local == "" || local == "_" || local == "." {
			continue // blank/dot imports never appear as a selector prefix
		}
		aliasToImport[local] = path
	}
	if len(aliasToImport) == 0 {
		return nil, nil
	}

	var exposures []Exposure
	report := func(name, kind string, typeExpr ast.Expr, pos token.Pos) {
		imp, ref := referencesAlias(typeExpr, aliasToImport)
		if !ref {
			return
		}
		p := fset.Position(pos)
		exposures = append(exposures, Exposure{
			File: filepath.Base(p.Filename), Line: p.Line, Name: name, Kind: kind,
			Type: types.ExprString(typeExpr), ImportPath: imp,
		})
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				kind := "type"
				if ts.Assign.IsValid() {
					kind = "alias"
				}
				// A struct/interface's own field/method-level exported
				// members are reported individually below (an unexported
				// field or method never leaks the type to a caller, since
				// Go's own visibility rules already hide it). Reporting
				// the whole container type here too would wrongly flag an
				// exported struct that merely holds an *unexported* pgx
				// field, so the top-level check only runs for every other
				// type form (alias, defined type, pointer/slice/map/chan
				// of the third-party type, and so on).
				switch t := ts.Type.(type) {
				case *ast.StructType:
					if t.Fields == nil {
						continue
					}
					for _, f := range t.Fields.List {
						if len(f.Names) == 0 {
							report(ts.Name.Name+".<embedded>", "field", f.Type, f.Type.Pos())
							continue
						}
						for _, n := range f.Names {
							if n.IsExported() {
								report(ts.Name.Name+"."+n.Name, "field", f.Type, f.Type.Pos())
							}
						}
					}
				case *ast.InterfaceType:
					if t.Methods == nil {
						continue
					}
					for _, m := range t.Methods.List {
						for _, n := range m.Names {
							if n.IsExported() {
								report(ts.Name.Name+"."+n.Name, "method", m.Type, m.Type.Pos())
							}
						}
					}
				default:
					report(ts.Name.Name, kind, ts.Type, ts.Type.Pos())
				}
			}
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = exprString(d.Recv.List[0].Type) + "." + name
			}
			if d.Type.Params != nil {
				for _, f := range d.Type.Params.List {
					report(name, "func", f.Type, f.Type.Pos())
				}
			}
			if d.Type.Results != nil {
				for _, f := range d.Type.Results.List {
					report(name, "func", f.Type, f.Type.Pos())
				}
			}
		}
	}
	return exposures, nil
}

func exprString(e ast.Expr) string { return types.ExprString(e) }

// referencesAlias reports whether expr contains a selector whose package
// identifier is one of aliasToImport's keys, walking through pointers,
// slices, maps, arrays, channels, func types, generic type arguments and
// struct/interface literals embedded in the expression.
func referencesAlias(expr ast.Expr, aliasToImport map[string]string) (string, bool) {
	var found string
	ast.Inspect(expr, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			if imp, ok := aliasToImport[id.Name]; ok {
				found = imp
				return false
			}
		}
		return true
	})
	return found, found != ""
}

func importLocalName(imp *ast.ImportSpec, path string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	segs := strings.Split(path, "/")
	name := segs[len(segs)-1]
	if len(segs) > 1 && isMajorVersionSuffix(name) {
		name = segs[len(segs)-2]
	}
	return name
}

func isMajorVersionSuffix(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// MatchesLeakType reports whether exp exposes importPath and its printed
// type expression names one of typeNames (e.g. "Tx", "Rows", "Conn" for
// pgx), matched as ".<Name>" so it also catches pointer/slice/map-wrapped
// forms like "*pgx.Conn" or "[]pgx.Rows".
func MatchesLeakType(exp Exposure, importPath string, typeNames []string) bool {
	if exp.ImportPath != importPath {
		return false
	}
	for _, name := range typeNames {
		if strings.Contains(exp.Type, "."+name) {
			return true
		}
	}
	return false
}

// FilterLeakTypes narrows exposures to only those matching MatchesLeakType
// for the given importPath/typeNames pair.
func FilterLeakTypes(exposures []Exposure, importPath string, typeNames []string) []Exposure {
	var out []Exposure
	for _, exp := range exposures {
		if MatchesLeakType(exp, importPath, typeNames) {
			out = append(out, exp)
		}
	}
	return out
}

func matchesAnyPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(path, p) {
				return true
			}
			continue
		}
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
