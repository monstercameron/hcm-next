// Package storeboundaries implements STORE-002: it proves that every store
// adapter under internal/data/** and internal/intent/app/pgstore/** either
// scopes its SQL to a tenant explicitly or runs only inside a
// tenancy.WithTenant-scoped transaction, that no adapter other than
// internal/data/pgxadapter opens its own PostgreSQL pool, and that no
// adapter writes a FIELD_LEVEL-classified column in plaintext without an
// encryption capability.
//
// # Why static text analysis, not a SQL parser
//
// The store adapters build SQL with fmt.Sprintf and plain string
// concatenation, not an ORM or query builder, so there is no single typed
// AST to walk for "the tables and columns this statement touches". This
// package instead treats each database call's SQL argument as text,
// resolves as much of it as static analysis of the surrounding Go source
// allows, and applies pattern-based rules to the result. Every heuristic
// below is named and documented at its point of use because each one is a
// deliberate, bounded approximation of an undecidable static question
// ("does this dynamically-built string touch tenant-scoped table X, and
// does it filter by tenant?") rather than a claim of precision. See
// sqlscan_test.go's fixtures for worked examples of what each heuristic
// does and does not catch.
package storeboundaries

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// SQLVerb is the leading DML/DDL keyword of a candidate SQL statement.
type SQLVerb string

const (
	VerbSelect SQLVerb = "SELECT"
	VerbInsert SQLVerb = "INSERT"
	VerbUpdate SQLVerb = "UPDATE"
	VerbDelete SQLVerb = "DELETE"
	VerbOther  SQLVerb = "OTHER"
)

// dynamicPlaceholder replaces a %s (or concatenation operand) this package's
// static analysis could not resolve to a literal string. It is a NUL byte,
// which cannot appear in an unquoted Go string literal or in a table/column
// name, so it can never accidentally complete a real identifier and cause a
// false table-name match.
const dynamicPlaceholder = "\x00"

// Statement is one candidate SQL call site: a string expression, found in a
// non-test .go file, whose resolved text begins with a DML/DDL verb.
type Statement struct {
	File     string        // path relative to the file's own package directory (informational)
	Func     string        // enclosing top-level function/method name, "" if none
	FuncDecl *ast.FuncDecl // enclosing top-level function, nil if none
	Verb     SQLVerb
	// RawTemplate is the literal source text before any %s/concatenation
	// substitution (the Sprintf format string, or the first leaf of a
	// concatenation chain, or the bare literal itself).
	RawTemplate string
	// Text is RawTemplate with every resolvable %s/concatenation operand
	// substituted in; unresolved operands become dynamicPlaceholder.
	Text string
	// ArgExprs is every Go expression that was substituted into Text (in
	// order), whether or not it resolved to a literal. Used by the tenant
	// scoping rule to look for "tenant" evidence in argument source text or
	// in the local variables an argument's expression is built from, even
	// when the value itself cannot be resolved to a compile-time constant.
	ArgExprs []ast.Expr
}

// unquoteLit returns a Go string literal's decoded value. ok is false for
// anything that is not a string BasicLit or fails to decode (which should
// not happen for anything go/parser accepted).
func unquoteLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// packageEnv is a package-wide map of identifier name to resolved string
// value, built from every top-level `const`/`var NAME = "literal"` (or a
// concatenation of literals) declaration across every file in the package.
// It approximates cross-file constant lookup (e.g. internal/data/outbox's
// selectRecordColumns, used from more than one file in principle) without a
// full types.Info build, which tools/policy packages deliberately avoid
// (see importgraph and garbagedrawer: both use go/parser + go list, never
// go/types, to stay fast over the whole tree).
func packageEnv(files map[string]*ast.File) map[string]string {
	env := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, name := range vs.Names {
					if v, ok := resolveLiteralExpr(vs.Values[i], nil); ok {
						env[name.Name] = v
					}
				}
			}
		}
	}
	return env
}

// resolveLiteralExpr resolves e to a compile-time string value using env
// (package-level) and, when non-nil, local (function-scoped, populated by
// localEnv). It handles a bare string literal, an identifier looked up in
// either environment, and a chain of string concatenation (`+`) whose every
// operand resolves.
func resolveLiteralExpr(e ast.Expr, local map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return unquoteLit(v)
	case *ast.Ident:
		if local != nil {
			if s, ok := local[v.Name]; ok {
				return s, true
			}
		}
		return "", false // package env is threaded in by the caller, see resolveArg
	case *ast.ParenExpr:
		return resolveLiteralExpr(v.X, local)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := resolveLiteralExpr(v.X, local)
		if !ok {
			return "", false
		}
		r, ok := resolveLiteralExpr(v.Y, local)
		if !ok {
			return "", false
		}
		return l + r, true
	}
	return "", false
}

// resolveArg resolves e against both the local (function-scoped) and
// package-wide environments, local taking precedence.
func resolveArg(e ast.Expr, pkgEnv, local map[string]string) (string, bool) {
	if v, ok := e.(*ast.Ident); ok {
		if local != nil {
			if s, ok := local[v.Name]; ok {
				return s, true
			}
		}
		if s, ok := pkgEnv[v.Name]; ok {
			return s, true
		}
		return "", false
	}
	if v, ok := e.(*ast.BinaryExpr); ok && v.Op == token.ADD {
		l, ok := resolveArg(v.X, pkgEnv, local)
		if !ok {
			return "", false
		}
		r, ok := resolveArg(v.Y, pkgEnv, local)
		if !ok {
			return "", false
		}
		return l + r, true
	}
	return resolveLiteralExpr(e, local)
}

// localEnv collects `name := "literal"` and `name := <resolvable-const>`
// short variable declarations inside fn's body, keyed by name. Best-effort:
// it does not track control flow or shadowing across blocks, which only
// ever makes this heuristic credit a statement it should not have (a
// false-negative violation-detection outcome, never a false accusation of a
// table it does not touch) because the only consumer, hasTenantEvidence,
// treats "found somewhere" as sufficient evidence.
func localEnv(fn *ast.FuncDecl, pkgEnv map[string]string) map[string]string {
	env := map[string]string{}
	if fn == nil || fn.Body == nil {
		return env
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if v, ok := resolveArg(as.Rhs[i], pkgEnv, env); ok {
				env[id.Name] = v
			}
		}
		return true
	})
	return env
}

// isFmtSprintf reports whether call is fmt.Sprintf(...).
func isFmtSprintf(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}

// verbAndAnchor reports the SQL verb of text if text (after trimming
// leading whitespace) begins with one of the recognized DML/DDL keywords,
// case-insensitively. Only an anchored match counts, so an ordinary log
// message or comment that merely contains the word "select" is never
// mistaken for a query: every hand-written SQL literal in this codebase is
// its own string starting directly with the verb (verified against the
// current tree; a CTE-leading `WITH ... SELECT` would need its own case if
// one is ever introduced -- none exist today, see the package doc).
func verbAndAnchor(text string) (SQLVerb, bool) {
	t := strings.TrimLeft(text, " \t\r\n")
	upper := strings.ToUpper(t)
	switch {
	case strings.HasPrefix(upper, "SELECT"):
		return VerbSelect, true
	case strings.HasPrefix(upper, "INSERT INTO") || strings.HasPrefix(upper, "INSERT"):
		return VerbInsert, true
	case strings.HasPrefix(upper, "UPDATE"):
		return VerbUpdate, true
	case strings.HasPrefix(upper, "DELETE"):
		return VerbDelete, true
	case strings.HasPrefix(upper, "CREATE TABLE"), strings.HasPrefix(upper, "TRUNCATE"):
		return VerbOther, true
	}
	return "", false
}

// flattenConcat splits a chain of string `+` BinaryExprs (left-associative,
// as Go parses them) into its ordered leaf operands.
func flattenConcat(e ast.Expr) []ast.Expr {
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return []ast.Expr{e}
	}
	return append(flattenConcat(bin.X), flattenConcat(bin.Y)...)
}

// collectStatements walks every top-level function/method declared in
// files (the package's own, non-test .go files) and returns every SQL
// candidate call site: an argument built with fmt.Sprintf, a chain of
// string concatenation, or a bare string literal, whose resolved text is
// anchored on a recognized SQL verb.
//
// Candidates are collected per top-level FuncDecl because a candidate's
// enclosing function is also the scope this package's tenant-evidence
// heuristics search for supporting local variables (see localEnv and
// exprHasTenantLiteral in rules.go); a closure inside that function is
// still covered since ast.Inspect over fn.Body descends into function
// literals too, and its free variables are declared in the same fn.Body
// this heuristic already scans.
func collectStatements(pkgEnv map[string]string, path string, file *ast.File) []Statement {
	var out []Statement
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		local := localEnv(fn, pkgEnv)
		funcName := fn.Name.Name
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			funcName = recvTypeName(fn.Recv.List[0].Type) + "." + funcName
		}

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.CallExpr:
				if isFmtSprintf(e) && len(e.Args) > 0 {
					format, ok := unquoteLit(e.Args[0])
					if !ok {
						return true // format built dynamically too; nothing to anchor on
					}
					verb, isSQL := verbAndAnchor(format)
					if !isSQL {
						return true
					}
					text, args := substitutePercentS(format, e.Args[1:], pkgEnv, local)
					out = append(out, Statement{File: path, Func: funcName, FuncDecl: fn, Verb: verb, RawTemplate: format, Text: text, ArgExprs: args})
					return false // do not also visit Args[0] as a bare literal below
				}
			case *ast.BinaryExpr:
				if e.Op != token.ADD {
					return true
				}
				leaves := flattenConcat(e)
				first, ok := unquoteLit(leaves[0])
				if !ok {
					return true
				}
				verb, isSQL := verbAndAnchor(first)
				if !isSQL {
					return true
				}
				var b strings.Builder
				var args []ast.Expr
				for i, leaf := range leaves {
					if i == 0 {
						b.WriteString(first)
						continue
					}
					if v, ok := resolveArg(leaf, pkgEnv, local); ok {
						b.WriteString(v)
					} else {
						b.WriteString(dynamicPlaceholder)
					}
					args = append(args, leaf)
				}
				out = append(out, Statement{File: path, Func: funcName, FuncDecl: fn, Verb: verb, RawTemplate: first, Text: b.String(), ArgExprs: args})
				return false
			case *ast.BasicLit:
				if e.Kind != token.STRING {
					return true
				}
				v, ok := unquoteLit(e)
				if !ok {
					return true
				}
				verb, isSQL := verbAndAnchor(v)
				if !isSQL {
					return true
				}
				out = append(out, Statement{File: path, Func: funcName, FuncDecl: fn, Verb: verb, RawTemplate: v, Text: v})
				return false
			}
			return true
		})
	}
	return out
}

// substitutePercentS replaces each literal "%s" in format, in order, with
// the corresponding arg's resolved value, or dynamicPlaceholder when an arg
// does not resolve to a compile-time string. Other verbs (%d, %v, ...) are
// left untouched: every table/column placeholder observed in this codebase
// uses %s (verified against the current tree; see the package doc).
func substitutePercentS(format string, args []ast.Expr, pkgEnv, local map[string]string) (string, []ast.Expr) {
	var b strings.Builder
	argi := 0
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) && format[i+1] == 's' {
			if argi < len(args) {
				if v, ok := resolveArg(args[argi], pkgEnv, local); ok {
					b.WriteString(v)
				} else {
					b.WriteString(dynamicPlaceholder)
				}
				argi++
			}
			i++
			continue
		}
		b.WriteByte(format[i])
	}
	return b.String(), args
}

// recvTypeName extracts a method's receiver type name, stripping a leading
// pointer star, for diagnostic Func names like "Appender.Append".
func recvTypeName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

// literalTableMentions returns every registered table name (from
// tableNames) that appears, character-for-character, as the entire value
// of some string literal anywhere in files -- in any context, not only
// inside a recognized SQL statement. It is Heuristic T (Tier-B table
// resolution): when a statement's own table placeholder cannot be resolved
// (it is a function parameter or a struct field, e.g.
// internal/data/aggregates's `put(ctx, ex, table string, ...)` called from
// sibling files as put(ctx, ex, "person", ...)), this package falls back to
// "every registered table this package is seen literally naming anywhere"
// as the statement's candidate table set, rather than leaving the
// statement unevaluated. This is deliberately coarse: it can credit or
// blame a dynamic-table statement for a table it does not actually touch
// this call, in either direction, when a package's literal table mentions
// span more than one table (aggregates does, on purpose: one shared put/
// currentAsOfSQL/knownAsOfSQL helper serves every bitemporal aggregate
// table). See rules.go's EvaluateTenantScope for how the two tiers combine.
func literalTableMentions(files map[string]*ast.File, tableNames map[string]bool) map[string]bool {
	found := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, ok := unquoteLit(lit)
			if !ok {
				return true
			}
			if tableNames[v] {
				found[v] = true
			}
			return true
		})
	}
	return found
}

// parseNonTestGoFiles parses every .go file in goFiles (already filtered to
// exclude _test.go by the caller) and returns them keyed by base file name.
func parseNonTestGoFiles(fset *token.FileSet, dir string, goFiles []string) (map[string]*ast.File, error) {
	out := make(map[string]*ast.File, len(goFiles))
	for _, name := range goFiles {
		full := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, full, nil, 0)
		if err != nil {
			return nil, err
		}
		out[name] = f
	}
	return out, nil
}
