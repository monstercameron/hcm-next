// Package integrationboundaries enforces ARCH-GO-021's integration ownership
// boundary.  It is intentionally a source check: the integration contract is
// about who may import whom, and cannot be proved by exercising a connector.
package integrationboundaries

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

// Finding is one source-level ownership violation.
type Finding struct {
	Code   string
	Path   string
	Line   int
	Detail string
}

// Violation is retained as a descriptive alias for callers that use the
// policy-checker vocabulary.
type Violation = Finding

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s (%s)", f.Path, f.Line, f.Detail, f.Code)
}

// Check walks root and checks production Go files in integration,
// connectivity, domain, and workflow subtrees.  Both the current
// connectivity name and the architecture document's integration name are
// accepted during the migration.  Generated, vendor, and test files are
// excluded.
func Check(root string) []Finding {
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		s := filepath.ToSlash(rel)
		inScope := strings.Contains(s, "/integration/") || strings.Contains(s, "/connectivity/") || strings.Contains(s, "/domains/") || strings.HasPrefix(s, "internal/workflow")
		if strings.HasPrefix(s, "vendor/") || strings.Contains(s, "/gen/") || !inScope {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	var out []Finding
	for _, path := range files {
		out = append(out, inspect(root, path)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func inspect(root, path string) []Finding {
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return []Finding{{Code: "parse-error", Path: rel, Detail: err.Error()}}
	}
	// The package directory is the component immediately before the filename.
	dir := filepath.ToSlash(filepath.Dir(rel))
	base := filepath.Base(dir)
	var out []Finding
	add := func(code, detail string, pos token.Pos) {
		out = append(out, Finding{Code: code, Path: rel, Line: fset.Position(pos).Line, Detail: detail})
	}
	imports := map[string]string{}
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, "\"")
		alias := filepath.Base(ip)
		if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
			alias = imp.Name.Name
		}
		imports[alias] = ip
	}

	if base == "mapping" {
		engine := false
		for _, ip := range imports {
			if strings.Contains(ip, "/engines/transformation") || strings.Contains(ip, "/engine/transform") {
				engine = true
			}
		}
		if !engine {
			for _, d := range f.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && (fn.Name.Name == "Execute" || fn.Name.Name == "eval") {
					add("duplicate-transform", "mapping implements transformation execution; delegate to the shared transformation engine", fn.Pos())
				}
				if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
					for _, s := range gd.Specs {
						n := s.(*ast.TypeSpec).Name.Name
						if n == "IR" || n == "Rule" || n == "Op" {
							add("duplicate-transform", "mapping declares a private transformation language type "+n, s.Pos())
						}
					}
				}
			}
		}
	}

	if strings.Contains(dir, "/domains/") || strings.HasPrefix(dir, "internal/workflow") {
		for _, spec := range f.Imports {
			ip := strings.Trim(spec.Path.Value, "\"")
			if providerImport(ip) {
				add("provider-leak", "domain/workflow imports provider or adapter package "+ip, spec.Pos())
			}
		}
	}
	if base == "observe" {
		for alias, ip := range imports {
			if providerImport(ip) {
				inspectExportedTypes(f, alias, ip, add)
			}
		}
	}
	if base == "reconcile" {
		for alias, ip := range imports {
			if providerImport(ip) {
				add("reconcile-provider-mutation", "reconciliation imports external provider directly: "+ip, f.Pos())
				inspectMutations(f, alias, ip, add)
			}
		}
	}
	return out
}

func providerImport(ip string) bool {
	l := strings.ToLower(ip)
	return strings.Contains(l, "/provider") || strings.Contains(l, "/adapters/") || strings.Contains(l, "/adapter/") || strings.Contains(l, "oauth") || strings.Contains(l, "stripe") || strings.Contains(l, "workday") || strings.Contains(l, "salesforce")
}

func inspectExportedTypes(f *ast.File, alias, ip string, add func(string, string, token.Pos)) {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.IsExported() {
			inspectSelector(fn.Type, alias, func(pos token.Pos) {
				add("provider-type-leak", "exported observation API exposes provider response type from "+ip, pos)
			})
		}
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts := s.(*ast.TypeSpec)
			if ts.Name.IsExported() {
				inspectSelector(ts.Type, alias, func(pos token.Pos) {
					add("provider-type-leak", "exported observation type exposes provider response type from "+ip, pos)
				})
			}
		}
	}
}

func inspectSelector(n ast.Node, alias string, hit func(token.Pos)) {
	ast.Inspect(n, func(n ast.Node) bool {
		s, ok := n.(*ast.SelectorExpr)
		if ok {
			if id, ok := s.X.(*ast.Ident); ok && id.Name == alias {
				hit(s.Pos())
			}
		}
		return true
	})
}

func inspectMutations(f *ast.File, alias, ip string, add func(string, string, token.Pos)) {
	mut := map[string]bool{"Create": true, "Update": true, "Delete": true, "Put": true, "Send": true, "Write": true, "Patch": true}
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || !mut[s.Sel.Name] {
			return true
		}
		id, ok := s.X.(*ast.Ident)
		if ok && id.Name == alias {
			add("reconcile-provider-mutation", "reconciliation mutates external state through "+ip+"."+s.Sel.Name, s.Pos())
		}
		return true
	})
}
