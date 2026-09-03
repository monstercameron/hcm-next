// Package compositionroot checks ARCH-GO-020's application wiring boundary.
// The checker is intentionally syntax based: it can run before the application
// package exists, and reports architectural smells without executing code.
package compositionroot

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourcePackage is a syntax-level package input. Files permits tests to scan
// fixtures without creating a temporary module.
type SourcePackage struct {
	ImportPath, Dir, Name, Source string
	Files                         []string
	Generated                     bool
}

// Rules describes the roots in which each architectural prohibition applies.
// Paths are module-relative (for example, internal/domains or cmd).
type Rules struct {
	Module           string
	CompositionRoots []string
	CommandRoots     []string
	BusinessRoots    []string
	AdapterRoots     []string
	IgnoredPackages  []string
}

// Violation is a stable, machine-readable finding.
type Violation struct{ Kind, Package, File, Symbol, Detail string }

func (v Violation) String() string {
	s := v.Package
	if v.File != "" {
		s += ":" + v.File
	}
	if v.Symbol != "" {
		s += ":" + v.Symbol
	}
	return fmt.Sprintf("%s: %s (%s)", s, v.Detail, v.Kind)
}

// CheckCompositionSources checks source packages without filesystem access.
func CheckCompositionSources(packages []SourcePackage, rules Rules) []Violation {
	var out []Violation
	for _, p := range packages {
		if p.Generated || ignored(p.ImportPath, rules.IgnoredPackages) {
			continue
		}
		files := p.Files
		if len(files) == 0 && p.Source != "" {
			files = []string{p.Source}
		}
		for i, src := range files {
			f, err := parser.ParseFile(token.NewFileSet(), fmt.Sprintf("file%d.go", i), src, 0)
			if err != nil {
				out = append(out, Violation{Kind: "syntax-error", Package: p.ImportPath, Detail: err.Error()})
				continue
			}
			fileName := ""
			if i < len(p.Files) {
				fileName = filepath.Base(fmt.Sprintf("file%d.go", i))
			}
			out = append(out, scanFile(p, f, rules, fileName)...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Symbol < b.Symbol
	})
	return out
}

// Scan scans the root module's non-test Go source and returns raw findings.
func Scan(root string, rules Rules) ([]Violation, error) {
	pkgs, err := listPackages(root)
	if err != nil {
		return nil, err
	}
	if rules.Module == "" {
		rules.Module = modulePath(root)
	}
	var src []SourcePackage
	for _, p := range pkgs {
		if p.ImportPath != rules.Module && !strings.HasPrefix(p.ImportPath, rules.Module+"/") {
			continue
		}
		ents, err := os.ReadDir(p.Dir)
		if err != nil {
			return nil, err
		}
		var files []string
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(p.Dir, e.Name()))
			if err != nil {
				return nil, err
			}
			files = append(files, string(b))
		}
		rel := strings.TrimPrefix(p.ImportPath, rules.Module+"/")
		src = append(src, SourcePackage{ImportPath: p.ImportPath, Dir: p.Dir, Files: files, Generated: strings.HasPrefix(rel, "gen/") || rel == "gen"})
	}
	return CheckCompositionSources(src, rules), nil
}

// ScanComposition is a compatibility alias used by quality harnesses.
func ScanComposition(root string, rules Rules) ([]Violation, error) { return Scan(root, rules) }

func scanFile(p SourcePackage, f *ast.File, r Rules, file string) []Violation {
	var out []Violation
	rel := p.ImportPath
	command := under(rel, r.CommandRoots)
	business := under(rel, r.BusinessRoots)
	root := under(rel, r.CompositionRoots)
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv == nil && x.Name.Name == "init" && !root {
				out = append(out, Violation{"init-registration", rel, file, "init", "package init function is hidden registration or startup wiring"})
			}
		case *ast.GenDecl:
			if x.Tok == token.VAR && !root {
				for _, s := range x.Specs {
					v := s.(*ast.ValueSpec)
					for i, n := range v.Names {
						// Registry-like globals are forbidden even when their zero
						// value is initialized lazily in a method.
						nameSuggestsRegistry := strings.Contains(strings.ToLower(n.Name), "registry") || strings.Contains(strings.ToLower(n.Name), "locator") || strings.Contains(strings.ToLower(n.Name), "providers")
						if nameSuggestsRegistry || (i < len(v.Values) && mutableGlobal(v.Values[i])) {
							out = append(out, Violation{"global-mutable-registry", rel, file, n.Name, "package-level mutable state must be constructed by the composition root"})
						}
					}
				}
			}
		}
	}
	adapterAliases := map[string]bool{}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		if command && (underImport(path, r.BusinessRoots, r.Module) || underImport(path, r.AdapterRoots, r.Module)) {
			out = append(out, Violation{"command-business-import", rel, file, "", fmt.Sprintf("command imports business/adapter package %q; commands only select a role and invoke application lifecycle", path)})
		}
		if business && underImport(path, r.AdapterRoots, r.Module) && imp.Name == nil {
			adapterAliases[filepath.Base(path)] = true
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if business {
			switch x := n.(type) {
			case *ast.CompositeLit:
				if sel, ok := x.Type.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok && adapterAliases[pkg.Name] {
						out = append(out, Violation{"concrete-adapter-construction", rel, file, sel.Sel.Name, "business package constructs a concrete adapter; wiring belongs in the composition root"})
					}
				}
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok && adapterAliases[pkg.Name] && strings.HasPrefix(sel.Sel.Name, "New") {
						out = append(out, Violation{"concrete-adapter-construction", rel, file, sel.Sel.Name, "business package constructs a concrete adapter; wiring belongs in the composition root"})
					}
				}
			}
		}
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name := callName(c.Fun); isLocator(name) && !root {
			out = append(out, Violation{"service-locator", rel, file, name, "ambient service lookup bypasses explicit dependency injection"})
		}
		return true
	})
	return out
}

func mutableGlobal(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.CompositeLit:
		return x.Type != nil && isMapSlice(x.Type)
	case *ast.CallExpr:
		n := callName(x.Fun)
		return n == "make" || n == "new" || strings.HasPrefix(n, "New") || strings.Contains(strings.ToLower(n), "registry")
	}
	return false
}
func isMapSlice(e ast.Expr) bool {
	switch e.(type) {
	case *ast.MapType, *ast.ArrayType:
		return true
	}
	return false
}
func callName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}
func isLocator(n string) bool {
	n = strings.ToLower(n)
	return strings.Contains(n, "servicelocator") || n == "locate" || n == "resolve" || n == "resolveprovider" || n == "getservice"
}
func under(path string, roots []string) bool {
	for _, r := range roots {
		r = strings.TrimSuffix(r, "/")
		if path == r || strings.HasPrefix(path, r+"/") {
			return true
		}
	}
	return false
}
func underImport(path string, roots []string, module string) bool {
	rel := strings.TrimPrefix(path, module+"/")
	return under(rel, roots)
}
func ignored(path string, roots []string) bool { return under(path, roots) }
