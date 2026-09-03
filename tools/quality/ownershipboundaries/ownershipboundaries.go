// Package ownershipboundaries checks ARCH-GO-022's human-interaction seams.
//
// The checker is intentionally source based: it verifies package ownership and
// provider isolation without requiring a running service or generated API.
package ownershipboundaries

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

// Finding is one ownership-boundary violation.
type Finding struct {
	Code   string
	Path   string
	Line   int
	Detail string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s (%s)", f.Path, f.Line, f.Detail, f.Code)
}

// Check scans production Go files below root and returns deterministic findings.
func Check(root string) []Finding {
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "vendor/") || strings.HasPrefix(rel, "gen/") || strings.Contains(rel, "/gen/") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	var out []Finding
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
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] != "internal" {
		return nil
	}
	owner := parts[1]
	if owner != "workflow" && owner != "work" && owner != "humanwork" && owner != "forms" && owner != "messaging" {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return []Finding{{Code: "parse-error", Path: rel, Detail: err.Error()}}
	}
	var out []Finding
	add := func(code, detail string, pos token.Pos) {
		out = append(out, Finding{Code: code, Path: rel, Line: fset.Position(pos).Line, Detail: detail})
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")
		if (owner == "workflow" || owner == "humanwork") && providerImport(path) {
			add("human-work-provider-import", "workflow/human-work package imports an email/SMS provider "+path, imp.Pos())
		}
	}
	for _, decl := range file.Decls {
		for _, exported := range exportedDecls(decl) {
			name, pos := exported.name, exported.pos
			if owner == "workflow" && workflowHumanWorkName(name) {
				add("workflow-owns-human-work", "workflow exports human-work responsibility "+name, pos)
			}
			if owner == "forms" && strings.Contains(strings.ToLower(name), "approval") {
				add("forms-owns-approval", "forms exports approval responsibility "+name, pos)
			}
			if owner == "messaging" && strings.Contains(strings.ToLower(name), "escalat") {
				add("messaging-owns-escalation", "messaging exports business escalation responsibility "+name, pos)
			}
		}
	}
	return out
}

type exported struct {
	name string
	pos  token.Pos
}

func exportedDecls(decl ast.Decl) []exported {
	var out []exported
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Name.IsExported() {
			out = append(out, exported{d.Name.Name, d.Name.Pos()})
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
				out = append(out, exported{ts.Name.Name, ts.Name.Pos()})
			}
			if vs, ok := spec.(*ast.ValueSpec); ok {
				for _, n := range vs.Names {
					if n.IsExported() {
						out = append(out, exported{n.Name, n.Pos()})
					}
				}
			}
		}
	}
	return out
}

func workflowHumanWorkName(name string) bool {
	l := strings.ToLower(name)
	for _, marker := range []string{"workitem", "work_item", "queue", "claim", "assignment", "assignee", "lease", "deadline", "escalat", "delegat", "sla"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

func providerImport(path string) bool {
	l := strings.ToLower(path)
	for _, marker := range []string{"net/smtp", "smtp", "email", "mailgun", "sendgrid", "twilio", "sms", "/ses", "ses-"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// CheckFile checks one source file, useful to policy tests and callers with a
// preselected package boundary.
func CheckFile(root, path string) []Finding { return inspectFile(root, path) }
