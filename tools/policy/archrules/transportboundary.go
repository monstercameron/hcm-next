// Package archrules (this file): ARCH-GO-023's transport-boundary checks.
//
// internal/transport decodes generated contracts, applies shared
// identity/governance/admission middleware, and forwards to the
// intent/capability/application ports declared in internal/transport/ports.go
// (or an equivalent port interface a transport subpackage declares for its
// own surface, e.g. internal/transport/admin's transport.IntentHandler
// reuse). It owns no business rule, no SQL and no concrete storage handle.
// This file supplies the AST-level scan four ways:
//
//   - TransportForbiddenImport: is one importer->imported edge a transport
//     package reaching into internal/domains/*, internal/engines/* or
//     internal/data/* directly (bypassing the declared port)?
//   - ScanTransportSource: does one file's source embed a SQL-shaped string
//     literal, reference a concrete pool/connection type, or declare an
//     RPC-handler-shaped method whose statement count exceeds the budget?
package archrules

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// TransportForbiddenRoots are the layers a transport package must reach only
// through a declared port, never by direct import.
var TransportForbiddenRoots = []string{
	"internal/domains",
	"internal/engines",
	"internal/data",
}

// TransportForbiddenImport reports whether importedRel falls under one of
// TransportForbiddenRoots, which makes importerRel -> importedRel an
// ARCH-GO-023 candidate violation (subject to the caller's allowlist for
// edges that are a reviewed, named exception rather than a bug).
func TransportForbiddenImport(importedRel string) bool {
	return UnderAnyRoot(importedRel, TransportForbiddenRoots)
}

// HandlerFinding is one scanned method shaped like a generated RPC handler
// (a receiver method named exported, taking (context.Context, *XRequest) and
// returning (*YResponse, error)) together with its statement count.
type HandlerFinding struct {
	Package    string
	File       string
	Func       string
	Statements int
}

// SQLStringFinding is one string literal in a transport file that has the
// shape of an embedded SQL statement (as opposed to, say, a redaction
// marker like the single word "select" that internal/transport/envelope
// uses to screen leaked provider text).
type SQLStringFinding struct {
	Package string
	File    string
	Literal string
}

// PoolUseFinding is one direct reference to a concrete database pool,
// connection or driver package inside a transport file.
type PoolUseFinding struct {
	Package string
	File    string
	Detail  string
}

// sqlStatementPattern requires a SQL clause keyword, an intervening
// identifier token (the table/column list a real query always names), and
// its paired closing keyword (e.g. "select <cols> ... from", "update <table>
// ... set") so a bare redaction marker phrase like envelope.go's
// unsafeMarkers ("select ", "update set", "delete from" with nothing else)
// never matches; a real embedded query always carries the intervening
// identifier a marker phrase omits.
var sqlStatementPattern = regexp.MustCompile(`(?is)\bselect\b\s+\S+.*?\bfrom\b|\binsert\s+into\b\s+\S+.*?\bvalues\b|\bupdate\b\s+\S+.*?\bset\b|\bdelete\s+from\b\s+\S+`)

// poolImportPrefixes are import paths that hand a caller a live connection
// or pool handle rather than an owner-declared port.
var poolImportPrefixes = []string{
	"github.com/jackc/pgx",
	"database/sql",
}

// poolSelectorPackages are package identifiers whose selector expressions
// (pkg.Symbol) name a concrete pool/connection type even when reached via an
// aliased or dot-style import path this scan's import-prefix check would
// miss.
var poolSelectorPackages = []string{"pgxpool"}

// ScanTransportSource parses one transport .go file's source and reports:
// every RPC-handler-shaped method with its statement count, every string
// literal shaped like an embedded SQL statement, and every direct reference
// to a concrete database pool/connection package or type. pkgRel and
// filename are attached to each finding for reporting only.
func ScanTransportSource(pkgRel, filename, source string) (handlers []HandlerFinding, sqlFindings []SQLStringFinding, poolFindings []PoolUseFinding, err error) {
	fset := token.NewFileSet()
	file, perr := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if perr != nil {
		return nil, nil, nil, fmt.Errorf("archrules: parsing %s: %w", filename, perr)
	}

	for _, imp := range file.Imports {
		path, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil {
			continue
		}
		for _, prefix := range poolImportPrefixes {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				poolFindings = append(poolFindings, PoolUseFinding{Package: pkgRel, File: filename, Detail: "imports " + path})
			}
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if looksLikeRPCHandler(node) && node.Body != nil {
				handlers = append(handlers, HandlerFinding{
					Package:    pkgRel,
					File:       filename,
					Func:       node.Name.Name,
					Statements: CountStatements(node.Body),
				})
			}
		case *ast.SelectorExpr:
			if ident, ok := node.X.(*ast.Ident); ok {
				for _, marker := range poolSelectorPackages {
					if ident.Name == marker {
						poolFindings = append(poolFindings, PoolUseFinding{Package: pkgRel, File: filename, Detail: "references " + marker + "." + node.Sel.Name})
					}
				}
			}
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				value, uerr := strconv.Unquote(node.Value)
				if uerr == nil && sqlStatementPattern.MatchString(value) {
					sqlFindings = append(sqlFindings, SQLStringFinding{Package: pkgRel, File: filename, Literal: value})
				}
			}
		}
		return true
	})

	return handlers, sqlFindings, poolFindings, nil
}

// looksLikeRPCHandler reports whether fn has the exact shape a generated
// gRPC service method requires: an exported method with a receiver, taking
// (context.Context, *Request) and returning (*Response, error). It is a
// syntax-only shape test (no type-checking), which is sufficient because
// every generated service interface in this repository follows this literal
// pattern.
func looksLikeRPCHandler(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	if !fn.Name.IsExported() {
		return false
	}
	params := fn.Type.Params
	if params == nil || len(params.List) != 2 {
		return false
	}
	if !isSelectorType(params.List[0].Type, "context", "Context") {
		return false
	}
	if _, ok := params.List[1].Type.(*ast.StarExpr); !ok {
		return false
	}
	results := fn.Type.Results
	if results == nil || len(results.List) != 2 {
		return false
	}
	if _, ok := results.List[0].Type.(*ast.StarExpr); !ok {
		return false
	}
	return isIdentType(results.List[1].Type, "error")
}

func isSelectorType(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg && sel.Sel.Name == name
}

func isIdentType(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

// CountStatements counts every statement inside body, recursing into nested
// blocks (an if/for/switch's own header statement counts once; the
// statements in its body count individually) without double-counting a
// *ast.BlockStmt node itself, which only groups other statements.
func CountStatements(body *ast.BlockStmt) int {
	if body == nil {
		return 0
	}
	count := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.BlockStmt); ok {
			return true
		}
		if _, ok := n.(ast.Stmt); ok {
			count++
		}
		return true
	})
	return count
}
