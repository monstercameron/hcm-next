package storeboundaries

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

// PackageSource is everything one rule needs about a single Go package: its
// import path, its direct imports, and its parsed non-test source files.
// Scan (policy.go) builds this from `go list -json` plus go/parser against
// the real tree; tests build it directly from in-memory source so the rule
// logic is exercised without touching the filesystem or invoking `go list`.
type PackageSource struct {
	ImportPath string
	Imports    []string
	Files      map[string]*ast.File
	Fset       *token.FileSet
}

// --- Rule 1: tenant scoping -------------------------------------------------

// TenantFinding is one RED-clause violation of the tenant-scoping rule: a
// statement touching a tenant-scoped registered table with neither an
// explicit tenant predicate nor a package-level signal that it only ever
// runs inside a tenancy.WithTenant-scoped transaction.
type TenantFinding struct {
	Package string
	File    string
	Func    string
	Verb    SQLVerb
	// Tables is the candidate table set this statement touches: exactly one
	// entry when the table name was resolved directly from the statement's
	// own text (Tier A), or every table this package is seen literally
	// naming elsewhere when the table was a runtime parameter this package
	// could not resolve (Tier B; see literalTableMentions in sqlscan.go).
	Tables []string
}

func (f TenantFinding) String() string {
	return fmt.Sprintf("%s: %s %s touches tenant-scoped table(s) %s with no explicit tenant predicate and no tenancy.WithTenant/dbport.Tx signal in package %s",
		f.File, f.Verb, f.Func, strings.Join(f.Tables, ","), f.Package)
}

var wherePattern = regexp.MustCompile(`(?is)\bWHERE\b`)
var tenantEqualsBind = regexp.MustCompile(`(?is)tenant_id\s*=\s*\$`)
var insertColumnList = regexp.MustCompile(`(?is)INSERT\s+INTO\s+\S+\s*\(([^)]*)\)`)
var tenantIDWord = regexp.MustCompile(`(?i)\btenant_id\b`)

// hasTenantEvidenceInText looks for an explicit tenant predicate in a
// statement's own (possibly partially substituted) text:
//   - INSERT: the column list right after the table name names tenant_id
//     (an INSERT naming tenant_id anywhere in its column list is always
//     evidence; there is no other reason that identifier would appear
//     there).
//   - SELECT/UPDATE/DELETE: the text following the first WHERE contains
//     `tenant_id = $n` (deliberately not "tenant_id appears anywhere",
//     which would also credit a SELECT that merely reads the tenant_id
//     column back out without filtering on it -- see bitemporal's
//     selectColumns, which does exactly that and must not get credit here
//     for a filter it does not apply).
func hasTenantEvidenceInText(verb SQLVerb, text string) bool {
	switch verb {
	case VerbInsert:
		m := insertColumnList.FindStringSubmatch(text)
		if m == nil {
			return false
		}
		return tenantIDWord.MatchString(m[1])
	case VerbSelect, VerbUpdate, VerbDelete:
		loc := wherePattern.FindStringIndex(text)
		if loc == nil {
			return false
		}
		return tenantEqualsBind.MatchString(text[loc[1]:])
	default:
		return false
	}
}

// argsMentionTenant is Heuristic H3/H4: evidence that a statement is
// tenant-scoped even though its own text could not be checked directly,
// because the WHERE clause (or column list) itself was built dynamically
// rather than appearing as literal text in the Sprintf/concatenation
// template.
//
//   - H3 (named argument): an argument's own Go source text contains
//     "tenant" case-insensitively, e.g. health/probe.go's
//     `fmt.Sprintf("SELECT count(*) FROM %s WHERE %s = $1", td.Table,
//     td.TenantScopingColumn)` -- the column name is itself unresolvable at
//     compile time (it comes from the registry, loaded at runtime), but no
//     other reason a %s argument in this position would be named
//     "TenantScopingColumn".
//   - H4 (built-up local variable): an argument references a local
//     variable (directly, or through strings.Join/strings.Builder) whose
//     construction anywhere in the enclosing function includes a string
//     literal containing "tenant_id", e.g. bitemporal/sql.go's
//     `where := []string{"e.tenant_id = " + ab.add(req.Tenant)}` followed
//     by `strings.Join(where, " AND ")` passed as the %s WHERE argument.
//
// Both directions only ever add evidence a statement is tenant-scoped; they
// never suppress a finding that hasTenantEvidenceInText would otherwise
// raise, so a false positive here can only hide a real gap behind
// unrelated argument naming, never accuse a correctly-scoped statement.
// That trade-off is deliberate: see the package doc's rationale for
// treating every heuristic as a bounded approximation reviewed through the
// allowlist, not a soundness proof.
func argsMentionTenant(fn *ast.FuncDecl, fset *token.FileSet, args []ast.Expr) bool {
	for _, arg := range args {
		if arg == nil {
			continue
		}
		var buf strings.Builder
		if fset != nil {
			_ = printer.Fprint(&buf, fset, arg)
		}
		if strings.Contains(strings.ToLower(buf.String()), "tenant") {
			return true
		}
		found := false
		ast.Inspect(arg, func(n ast.Node) bool {
			if found {
				return false
			}
			if id, ok := n.(*ast.Ident); ok && fn != nil {
				if localVarContainsTenantLiteral(fn, id.Name) {
					found = true
					return false
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// localVarContainsTenantLiteral reports whether any assignment to name
// inside fn's body (a `name := ...`, or a later `name = ...`/
// `name = append(name, ...)`) builds its value from a string literal
// containing "tenant_id" anywhere in the assigned expression's subtree.
func localVarContainsTenantLiteral(fn *ast.FuncDecl, name string) bool {
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != name {
				continue
			}
			var rhs ast.Expr
			switch {
			case len(as.Rhs) == len(as.Lhs):
				rhs = as.Rhs[i]
			case len(as.Rhs) == 1:
				rhs = as.Rhs[0]
			}
			if rhs != nil && exprHasTenantLiteral(rhs) {
				found = true
			}
		}
		return true
	})
	return found
}

// exprHasTenantLiteral reports whether e's subtree contains a string
// literal whose value contains "tenant_id" (case-insensitive).
func exprHasTenantLiteral(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if v, ok := unquoteLit(lit); ok && tenantIDWord.MatchString(v) {
			found = true
		}
		return true
	})
	return found
}

// dbportTxParamName is the exact identifier a parameter's type must render
// as (via go/printer) to count as "receives a dbport.Tx" for the package
// heuristic below. It intentionally does not match a package-local
// interface that merely embeds dbport.Execer/dbport.Querier (as
// internal/data/aggregates.Executor and internal/data/workforce's local
// interface both do): such an interface is also satisfiable by a bare
// dbport.Conn with no transaction and no tenancy.WithTenant call, so it
// gives no static proof of the RED clause's "runs only inside a
// tenancy.WithTenant-scoped transaction" -- exactly the STORE-002 TEST
// clause's documented approximation. Packages that hit this gap are
// reviewed in allowlist.yaml.
const dbportTxParamName = "dbport.Tx"

// hasDbportTxParam reports whether any function or method in files declares
// a parameter whose type is literally dbport.Tx.
func hasDbportTxParam(fset *token.FileSet, files map[string]*ast.File) bool {
	for _, f := range files {
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			if found {
				return false
			}
			ft, ok := n.(*ast.FuncType)
			if !ok || ft.Params == nil {
				return true
			}
			for _, field := range ft.Params.List {
				var buf strings.Builder
				_ = printer.Fprint(&buf, fset, field.Type)
				if buf.String() == dbportTxParamName {
					found = true
					return false
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// importsTenancy reports whether pkgImports contains
// internal/data/tenancy.
func importsTenancy(pkgImports []string) bool {
	for _, imp := range pkgImports {
		if strings.HasSuffix(imp, "/internal/data/tenancy") || imp == "internal/data/tenancy" {
			return true
		}
	}
	return false
}

// EvaluateTenantScope runs the STORE-002 tenant-scoping rule (TEST clause
// (1)) over one package. reg is the STORE-001 storage-disposition registry;
// only tables it marks tenant-scoped (TenantScoped() true) are in scope --
// a statement touching a control-plane or non-tenant table is never a
// finding here regardless of what its text contains.
func EvaluateTenantScope(pkg PackageSource, reg *storagedisposition.Registry) []TenantFinding {
	tenantTables := map[string]bool{}
	allTables := map[string]bool{}
	for _, t := range reg.Tables {
		allTables[t.Table] = true
		if t.TenantScoped() {
			tenantTables[t.Table] = true
		}
	}

	pkgEnv := packageEnv(pkg.Files)
	tierBCandidates := literalTableMentions(pkg.Files, allTables)
	pkgFallback := importsTenancy(pkg.Imports) || hasDbportTxParam(pkg.Fset, pkg.Files)

	var findings []TenantFinding
	for name, file := range pkg.Files {
		for _, stmt := range collectStatements(pkgEnv, name, file) {
			tables := matchTables(stmt.Text, tenantTables)
			tierB := false
			if len(tables) == 0 && strings.Contains(stmt.Text, dynamicPlaceholder) {
				for t := range tierBCandidates {
					if tenantTables[t] {
						tables = append(tables, t)
						tierB = true
					}
				}
			}
			if len(tables) == 0 {
				continue // touches no registered tenant-scoped table we can identify
			}
			sort.Strings(tables)

			evidence := hasTenantEvidenceInText(stmt.Verb, stmt.Text) ||
				argsMentionTenant(stmt.FuncDecl, pkg.Fset, stmt.ArgExprs) ||
				pkgFallback
			if evidence {
				continue
			}
			_ = tierB // Tier B is only a table-identification fallback; evidence rules are identical either way.
			findings = append(findings, TenantFinding{
				Package: pkg.ImportPath,
				File:    name,
				Func:    stmt.Func,
				Verb:    stmt.Verb,
				Tables:  tables,
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Func != findings[j].Func {
			return findings[i].Func < findings[j].Func
		}
		return findings[i].String() < findings[j].String()
	})
	return findings
}

// matchTables returns every name in tenantTables that appears as a whole
// word in text.
func matchTables(text string, tenantTables map[string]bool) []string {
	var out []string
	for t := range tenantTables {
		if wholeWordContains(text, t) {
			out = append(out, t)
		}
	}
	return out
}

func wholeWordContains(text, word string) bool {
	idx := 0
	for {
		i := strings.Index(text[idx:], word)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(word)
		beforeOK := start == 0 || !isIdentByte(text[start-1])
		afterOK := end == len(text) || !isIdentByte(text[end])
		if beforeOK && afterOK {
			return true
		}
		idx = start + 1
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// WholeWordContains reports whether word occurs in text outside an identifier.
// It is shared by policy packages that must apply the same identifier-boundary
// rule when matching table or tenant-column names.
func WholeWordContains(text, word string) bool { return wholeWordContains(text, word) }

// IsIdentifierByte reports whether b can be part of a SQL/Go identifier.
func IsIdentifierByte(b byte) bool { return isIdentByte(b) }

// --- Rule 2: no adapter opens its own pool ----------------------------------

// pgxpoolImportPath is the exact import path only internal/data/pgxadapter
// may use.
const pgxpoolImportPath = "github.com/jackc/pgx/v5/pgxpool"

// pgxadapterImportPath is the one package allowed to import pgxpoolImportPath.
const pgxadapterImportPath = "github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"

// PoolFinding is a RED-clause violation of "no adapter opens its own pool".
type PoolFinding struct {
	Package string
}

func (f PoolFinding) String() string {
	return fmt.Sprintf("%s: imports %s directly; only %s may", f.Package, pgxpoolImportPath, pgxadapterImportPath)
}

// EvaluatePoolImport runs the STORE-002 pool-ownership rule (TEST clause
// (2)) over one package.
func EvaluatePoolImport(pkg PackageSource) *PoolFinding {
	if pkg.ImportPath == pgxadapterImportPath {
		return nil
	}
	for _, imp := range pkg.Imports {
		if imp == pgxpoolImportPath {
			return &PoolFinding{Package: pkg.ImportPath}
		}
	}
	return nil
}

// --- Rule 3: encryption class -----------------------------------------------

// EncryptionGap is the STORE-002 TEST clause (3) report: today, no adapter
// package under internal/data or internal/kernel exports an interface with
// an Encrypt/Decrypt method (PortEvidence is empty), and the registry
// currently classifies zero tables FIELD_LEVEL (FieldLevelTables is empty).
// Those two facts are consistent with each other: there being nothing to
// enforce is exactly why nothing enforces it. The moment either changes on
// its own -- a FIELD_LEVEL table appears with still no port, or a port
// appears that some FIELD_LEVEL write does not go through -- this
// consistency breaks and TestTodo_STORE_002 must fail until a real
// encryption-boundary rule replaces this registry-consistency check.
type EncryptionGap struct {
	FieldLevelTables []string
	PortEvidence     []string
}

// Consistent reports whether today's "nothing to enforce" state holds:
// FieldLevelTables and PortEvidence are either both empty (no gap yet) or
// PortEvidence is non-empty (a port exists, so a real per-write rule is
// possible and this package's report gap should be closed instead).
func (g EncryptionGap) Consistent() bool {
	return len(g.FieldLevelTables) == 0 || len(g.PortEvidence) > 0
}

// encryptMethodPattern matches an interface method plausibly named for an
// encryption capability.
var encryptMethodPattern = regexp.MustCompile(`(?i)^(encrypt|decrypt)`)

// FindEncryptionPort scans files (parsed from internal/data and
// internal/kernel by the caller) for an exported interface type with a
// method named Encrypt* or Decrypt*, and returns "path#TypeName" for every
// match. An empty result means this codebase has no encryption capability
// port for STORE-002's TEST clause (3) to require adapters to hold.
func FindEncryptionPort(files map[string]*ast.File) []string {
	var out []string
	for path, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok || it.Methods == nil {
					continue
				}
				for _, m := range it.Methods.List {
					for _, n := range m.Names {
						if encryptMethodPattern.MatchString(n.Name) {
							out = append(out, path+"#"+ts.Name.Name)
						}
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}
