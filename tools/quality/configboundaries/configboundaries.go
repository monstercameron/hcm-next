// Package configboundaries enforces ARCH-GO-024's configuration ownership
// boundaries. Configuration selects behaviour; it does not execute domain
// transactions, authored definitions are inputs, and rollout consumes
// immutable bundles rather than rewriting them.
package configboundaries

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

const (
	DefinitionsRuntimeImport          = "definitions-runtime-import"
	ConfigurationDomainImport         = "configuration-domain-import"
	ConfigurationStorageImport        = "configuration-storage-import"
	ConfigurationExecutesDomain       = "configuration-executes-domain"
	RolloutRewritesBundle             = "rollout-rewrites-bundle"
	CustomerConfigBypassesPublication = "customer-config-bypasses-publication"
)

type Finding struct {
	Code, Path, Detail string
	Line               int
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s (%s)", f.Path, f.Line, f.Detail, f.Code)
}

// Check scans production Go sources beneath root. It is read-only and returns
// findings in stable path/line/code order. Test, generated, and vendor files
// are deliberately excluded because they are not runtime ownership edges.
func Check(root string) []Finding {
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "vendor/") || strings.Contains(rel, "/vendor/") || strings.HasPrefix(rel, "gen/") || strings.Contains(rel, "/gen/") {
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

func CheckFile(root, path string) []Finding { return inspect(root, path) }

func inspect(root, path string) []Finding {
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, nil, parser.ParseComments)
	if err != nil {
		return []Finding{{Code: "parse-error", Path: rel, Detail: err.Error()}}
	}
	imports := make([]struct {
		path string
		pos  token.Pos
	}, 0, len(file.Imports))
	for _, imp := range file.Imports {
		imports = append(imports, struct {
			path string
			pos  token.Pos
		}{strings.Trim(imp.Path.Value, "\""), imp.Pos()})
	}
	var out []Finding
	add := func(code, detail string, pos token.Pos) {
		out = append(out, Finding{Code: code, Path: rel, Line: set.Position(pos).Line, Detail: detail})
	}
	role := packageRole(rel)
	for _, imp := range imports {
		switch {
		case role == "definitions" && runtimeConfigImport(imp.path):
			add(DefinitionsRuntimeImport, "authored definitions must not import runtime configuration", imp.pos)
		case role == "configuration" && domainImport(imp.path):
			add(ConfigurationDomainImport, "configuration owns semantics but must not depend on concrete domain execution", imp.pos)
		case role == "configuration" && storageImport(imp.path):
			add(ConfigurationStorageImport, "configuration must not own persistence or concrete stores", imp.pos)
		}
	}
	if role == "configuration" {
		ast.Inspect(file, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncDecl); ok && domainExecution(fn.Name.Name) {
				add(ConfigurationExecutesDomain, "configuration declares domain execution: "+fn.Name.Name, fn.Pos())
			}
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callName(c.Fun)
			if domainExecution(name) {
				add(ConfigurationExecutesDomain, "configuration executes a domain change: "+name, c.Pos())
			}
			return true
		})
	}
	if role == "rollout" {
		ast.Inspect(file, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if ok && bundleMutation(callName(c.Fun)) {
				add(RolloutRewritesBundle, "rollout must target immutable bundle versions; it must not rewrite bundles", c.Pos())
			}
			return true
		})
	}
	if role == "customer" {
		ast.Inspect(file, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if ok && bypassPublication(callName(c.Fun)) {
				add(CustomerConfigBypassesPublication, "customer configuration must pass through publication and approval", c.Pos())
			}
			return true
		})
	}
	return out
}

func packageRole(rel string) string {
	p := strings.Split(rel, "/")
	if len(p) == 0 {
		return ""
	}
	for _, s := range p {
		l := strings.ToLower(s)
		switch {
		case l == "definitions":
			return "definitions"
		case l == "configuration" || l == "config":
			return "configuration"
		case l == "rollout" || l == "rollouts":
			return "rollout"
		case l == "customer" || l == "customers":
			return "customer"
		}
	}
	return ""
}
func runtimeConfigImport(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "/internal/configuration") || strings.Contains(l, "/internal/config/") || strings.HasSuffix(l, "/configuration")
}
func domainImport(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "/internal/domains/") || strings.Contains(l, "/internal/domain/") || strings.Contains(l, "/internal/workflow/") || strings.Contains(l, "/internal/intent/")
}
func storageImport(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "/internal/data/") || strings.Contains(l, "/internal/store") || strings.Contains(l, "/internal/persistence")
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
func domainExecution(n string) bool {
	l := strings.ToLower(n)
	for _, m := range []string{"execute", "apply", "commit", "mutate", "transaction", "create", "update", "delete", "save", "write"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}
func bundleMutation(n string) bool {
	l := strings.ToLower(n)
	for _, m := range []string{"rewritebundle", "mutatebundle", "updatebundle", "writebundle", "replacebundle", "setbundle"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}
func bypassPublication(n string) bool {
	l := strings.ToLower(n)
	for _, m := range []string{"activate", "publish", "approve", "promote", "distribute"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}
