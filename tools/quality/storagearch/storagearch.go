// Package storagearch checks ARCH-GO-025's storage boundary.
//
// The checker is deliberately a small, dependency-free source check. It does
// not move packages or prescribe a database schema: semantic packages own
// ports, while technology packages are permitted to implement them below a
// composition boundary.
package storagearch

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is one ARCH-GO-025 violation.
type Finding struct {
	Code   string
	Path   string
	Line   int
	Detail string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s (%s)", f.Path, f.Line, f.Detail, f.Code)
}

// Check walks root and returns deterministic findings. root is normally the
// repository root. Only production Go files are checked; *_test.go files are
// fixtures and may legitimately bind concrete adapters.
func Check(root string) []Finding {
	var out []Finding
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "vendor/") || strings.Contains(filepath.ToSlash(rel), "/gen/") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	for _, path := range files {
		out = append(out, inspectFile(root, path)...)
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

func inspectFile(root, path string) []Finding {
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, "internal/") {
		return nil
	}
	isAdapter := technologyPath(rel)
	semantic := semanticPath(rel)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return []Finding{{Code: "parse-error", Path: rel, Detail: err.Error()}}
	}
	var out []Finding
	add := func(code, detail string, pos token.Pos) {
		p := fset.Position(pos)
		out = append(out, Finding{Code: code, Path: rel, Line: p.Line, Detail: detail})
	}
	for _, imp := range file.Imports {
		ip := strings.Trim(imp.Path.Value, "\"")
		if semantic && technologyImport(ip) {
			add("semantic-imports-technology", "semantic package imports concrete storage technology "+ip, imp.Pos())
		}
		// Driver imports are expected here; the semantic-import rule above is
		// what prevents them from crossing into an owner/use-case package.
	}
	if isAdapter {
		inspectAdapter(file, add)
	}
	return out
}

func inspectAdapter(file *ast.File, add func(string, string, token.Pos)) {
	aliases := map[string]bool{}
	for _, imp := range file.Imports {
		ip := strings.Trim(imp.Path.Value, "\"")
		if driverImport(ip) {
			name := filepath.Base(ip)
			if strings.HasPrefix(ip, "github.com/jackc/pgx") {
				name = "pgx"
			}
			if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
				name = imp.Name.Name
			}
			aliases[name] = true
		}
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.IsExported() && businessName(fn.Name.Name) {
			add("adapter-business-authority", "adapter exports business command/invariant "+fn.Name.Name, fn.Pos())
		}
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts := s.(*ast.TypeSpec)
			if ts.Name.IsExported() && businessName(ts.Name.Name) {
				add("adapter-business-authority", "adapter exports business type "+ts.Name.Name, ts.Pos())
			}
			if _, ok := ts.Type.(*ast.InterfaceType); ok && ts.Name.IsExported() {
				add("adapter-declares-port", "exported interface belongs beside its semantic consumer, not in adapter", ts.Pos())
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		se, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := se.X.(*ast.Ident)
		if ok && aliases[id.Name] && exportedContext(file, n) {
			add("driver-leak", "driver/client type appears in an exported adapter API", n.Pos())
		}
		return true
	})
}

func exportedContext(file *ast.File, n ast.Node) bool {
	for _, d := range file.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Name.IsExported() && contains(x.Type, n) {
				return true
			}
		case *ast.GenDecl:
			for _, s := range x.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.IsExported() && contains(ts.Type, n) {
					return true
				}
			}
		}
	}
	return false
}
func contains(root, needle ast.Node) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if n == needle {
			found = true
			return false
		}
		return !found
	})
	return found
}
func businessName(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "invariant") || strings.Contains(l, "command") || strings.HasPrefix(l, "execute") || strings.HasPrefix(l, "apply")
}
func technologyPath(p string) bool {
	for _, s := range strings.Split(p, "/") {
		if s == "postgres" || s == "object" || s == "cache" || s == "pgxadapter" || s == "adapters" {
			return true
		}
	}
	return strings.HasPrefix(p, "internal/data/store")
}
func technologyImport(p string) bool {
	return strings.Contains(p, "/internal/data/postgres") || strings.Contains(p, "/internal/data/object") || strings.Contains(p, "/internal/data/cache") || strings.Contains(p, "/internal/data/pgxadapter") || strings.Contains(p, "/internal/data/store")
}
func driverImport(p string) bool {
	return strings.HasPrefix(p, "github.com/jackc/pgx") || p == "database/sql" || strings.Contains(p, "/go-redis") || strings.Contains(p, "/minio-go")
}
func semanticPath(p string) bool {
	for _, root := range []string{"intent", "workflow", "ledger", "domains", "transaction", "humanwork"} {
		if strings.HasPrefix(p, "internal/"+root+"/") || p == "internal/"+root {
			return true
		}
	}
	return false
}

// CheckFile is useful to policy tests and callers that already have a file.
func CheckFile(root, path string) []Finding { return inspectFile(root, path) }

// ReadLines returns source lines for small policy diagnostics.
func ReadLines(path string) ([]string, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines, s.Err()
}
