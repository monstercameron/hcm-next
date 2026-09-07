// Package regexhoist checks that regular expressions are compiled once, at
// package level, rather than inside function bodies where every call pays
// the compilation again.
//
// A call inside a function body is allowed only when the pattern is built
// from runtime input and the line carries the marker comment
// "regexhoist:dynamic", which makes the exemption visible at the call site.
// Aliasing the compiler into a package-level variable (for example
// "var compile = regexp.MustCompile") is reported as a finding, because it
// hides a per-call compile from this check without removing it.
package regexhoist

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

// DynamicMarker is the comment text that exempts one call line.
const DynamicMarker = "regexhoist:dynamic"

// Finding is one compile site that violates the policy.
type Finding struct {
	Path string
	Line int
	Kind string // "function-body" or "alias"
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d (%s)", f.Path, f.Line, f.Kind) }

// Scan walks the given package directories (relative to root unless
// absolute), skipping testdata and _test.go files, and returns every
// unexempted compile call inside a function body and every package-level
// alias of a regexp compiler.
func Scan(root string, packageDirs []string) ([]Finding, error) {
	var findings []Finding
	for _, packageDir := range packageDirs {
		dir := packageDir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, filepath.FromSlash(packageDir))
		}
		if err := scanDir(root, dir, &findings); err != nil {
			return nil, err
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

func scanDir(root, dir string, findings *[]Finding) error {
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != dir && entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		return scanFile(root, path, findings)
	})
}

func scanFile(root, path string, findings *[]Finding) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	exempt := map[int]bool{}
	for _, group := range file.Comments {
		for _, c := range group.List {
			if strings.Contains(c.Text, DynamicMarker) {
				exempt[fset.Position(c.Pos()).Line] = true
			}
		}
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, value := range vs.Values {
					if isCompilerSelector(value) {
						*findings = append(*findings, Finding{Path: rel, Line: fset.Position(value.Pos()).Line, Kind: "alias"})
					}
				}
			}
		case *ast.FuncDecl:
			if d.Body == nil || d.Name.Name == "init" {
				continue
			}
			ast.Inspect(d.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !isCompilerSelector(call.Fun) {
					return true
				}
				line := fset.Position(call.Pos()).Line
				if exempt[line] {
					return true
				}
				*findings = append(*findings, Finding{Path: rel, Line: line, Kind: "function-body"})
				return true
			})
		}
	}
	return nil
}

// isCompilerSelector reports whether expr is regexp.Compile or
// regexp.MustCompile (the selector itself, not a call of it).
func isCompilerSelector(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel == nil {
		return false
	}
	if selector.Sel.Name != "Compile" && selector.Sel.Name != "MustCompile" {
		return false
	}
	packageIdent, ok := selector.X.(*ast.Ident)
	return ok && packageIdent.Name == "regexp"
}
