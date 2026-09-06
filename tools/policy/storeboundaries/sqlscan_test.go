package storeboundaries

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestVerbAndAnchor(t *testing.T) {
	cases := []struct {
		text string
		verb SQLVerb
		ok   bool
	}{
		{"SELECT 1", VerbSelect, true},
		{"  \n\tselect x from y", VerbSelect, true},
		{"INSERT INTO t (a) VALUES ($1)", VerbInsert, true},
		{"insert into t (a) values ($1)", VerbInsert, true},
		{"UPDATE t SET a = 1", VerbUpdate, true},
		{"DELETE FROM t", VerbDelete, true},
		{"CREATE TABLE t (a int)", VerbOther, true},
		{"this comment mentions select in passing", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		verb, ok := verbAndAnchor(c.text)
		if ok != c.ok || (ok && verb != c.verb) {
			t.Errorf("verbAndAnchor(%q) = (%q, %v), want (%q, %v)", c.text, verb, ok, c.verb, c.ok)
		}
	}
}

func TestWholeWordContains(t *testing.T) {
	cases := []struct {
		text, word string
		want       bool
	}{
		{"SELECT * FROM person WHERE x=1", "person", true},
		{"SELECT * FROM personnel WHERE x=1", "person", false},
		{"SELECT * FROM organization_unit", "organization_unit", true},
		{"SELECT * FROM organization_unit_history", "organization_unit", false},
		{"job", "job", true},
		{"jobs", "job", false},
		{"a_job_b", "job", false},
	}
	for _, c := range cases {
		if got := wholeWordContains(c.text, c.word); got != c.want {
			t.Errorf("wholeWordContains(%q, %q) = %v, want %v", c.text, c.word, got, c.want)
		}
	}
}

func TestHasTenantEvidenceInText(t *testing.T) {
	cases := []struct {
		name string
		verb SQLVerb
		text string
		want bool
	}{
		{"select with tenant filter", VerbSelect, "SELECT a FROM t WHERE tenant_id = $1 AND b = $2", true},
		{"select with no-space tenant filter", VerbSelect, "select a from t where tenant_id=$1", true},
		{"select without where", VerbSelect, "SELECT tenant_id, a FROM t ORDER BY a", false},
		{"select filtering on something else", VerbSelect, "SELECT a FROM t WHERE row_id = $1", false},
		{"update with tenant filter", VerbUpdate, "UPDATE t SET a = $1 WHERE tenant_id = $2 AND b = $3", true},
		{"update keyed by row id only", VerbUpdate, "UPDATE t SET a = $1 WHERE row_id = $2", false},
		{"delete with tenant filter", VerbDelete, "DELETE FROM t WHERE tenant_id = $1", true},
		{"insert naming tenant_id", VerbInsert, "INSERT INTO t (tenant_id, a) VALUES ($1, $2)", true},
		{"insert not naming tenant_id", VerbInsert, "INSERT INTO t (a, b) VALUES ($1, $2)", false},
		{"other verb", VerbOther, "CREATE TABLE t (tenant_id uuid)", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasTenantEvidenceInText(c.verb, c.text); got != c.want {
				t.Errorf("hasTenantEvidenceInText(%v, %q) = %v, want %v", c.verb, c.text, got, c.want)
			}
		})
	}
}

func mustParseFile(t *testing.T, fset *token.FileSet, name, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	return f
}

// TestCollectStatements_SprintfTableSubstitution proves the Sprintf %s
// resolution path: a literal table name argument gets substituted into the
// resolved Text, so a Tier A whole-word table match can find it later.
func TestCollectStatements_SprintfTableSubstitution(t *testing.T) {
	src := `package fixture

import "fmt"

func Get(ex Executor, table string) {
	ex.Query(fmt.Sprintf("SELECT a FROM %s WHERE tenant_id = $1", "widget"))
}
`
	fset := token.NewFileSet()
	f := mustParseFile(t, fset, "fixture.go", src)
	stmts := collectStatements(nil, "fixture.go", f)
	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1: %+v", len(stmts), stmts)
	}
	want := "SELECT a FROM widget WHERE tenant_id = $1"
	if stmts[0].Text != want {
		t.Errorf("Text = %q, want %q", stmts[0].Text, want)
	}
	if stmts[0].Verb != VerbSelect {
		t.Errorf("Verb = %v, want SELECT", stmts[0].Verb)
	}
}

// TestCollectStatements_ConcatChain proves the `"literal" + ident +
// "literal"` concatenation path used throughout internal/data/outbox,
// internal/data/ledger and internal/intent/app/pgstore (e.g.
// `SELECT `+selectRecordColumns+` FROM outbox WHERE tenant_id = $1`).
func TestCollectStatements_ConcatChain(t *testing.T) {
	src := `package fixture

const cols = "a, b, tenant_id"

func Get() string {
	return "SELECT " + cols + " FROM widget WHERE tenant_id = $1"
}
`
	fset := token.NewFileSet()
	f := mustParseFile(t, fset, "fixture.go", src)
	env := packageEnv(map[string]*ast.File{"fixture.go": f})
	stmts := collectStatements(env, "fixture.go", f)
	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1: %+v", len(stmts), stmts)
	}
	want := "SELECT a, b, tenant_id FROM widget WHERE tenant_id = $1"
	if stmts[0].Text != want {
		t.Errorf("Text = %q, want %q", stmts[0].Text, want)
	}
}

// TestCollectStatements_UnresolvedTableIsPlaceholder proves a dynamic
// (function-parameter) table name is left as the dynamic placeholder
// rather than silently dropped or mismatched, so downstream Tier A
// matching correctly fails to find it and Tier B (package-wide literal
// table mentions) is what has to pick it up -- see
// storeboundaries_test.go's TestTodo_STORE_002_Fault subtest "dynamic
// table resolved via Tier B is still caught".
func TestCollectStatements_UnresolvedTableIsPlaceholder(t *testing.T) {
	src := `package fixture

import "fmt"

func get(ex Executor, table string) {
	ex.Query(fmt.Sprintf("SELECT a FROM %s WHERE row_id = $1", table))
}
`
	fset := token.NewFileSet()
	f := mustParseFile(t, fset, "fixture.go", src)
	stmts := collectStatements(nil, "fixture.go", f)
	if len(stmts) != 1 {
		t.Fatalf("got %d statements, want 1: %+v", len(stmts), stmts)
	}
	if want := "SELECT a FROM " + dynamicPlaceholder + " WHERE row_id = $1"; stmts[0].Text != want {
		t.Errorf("Text = %q, want %q", stmts[0].Text, want)
	}
}

func TestLiteralTableMentions(t *testing.T) {
	src := `package fixture

func callers() {
	put("person")
	put("worker")
	notATable("something_else")
}
`
	fset := token.NewFileSet()
	f := mustParseFile(t, fset, "fixture.go", src)
	found := literalTableMentions(map[string]*ast.File{"fixture.go": f}, map[string]bool{"person": true, "worker": true, "job": true})
	if !found["person"] || !found["worker"] || found["job"] || len(found) != 2 {
		t.Errorf("literalTableMentions = %v, want exactly {person, worker}", found)
	}
}

func TestLocalVarContainsTenantLiteral(t *testing.T) {
	src := `package fixture

import "strings"

func build(tenant string) string {
	where := []string{"e.tenant_id = " + tenant}
	where = append(where, "e.other = 1")
	return strings.Join(where, " AND ")
}

func buildNoTenant() string {
	where := []string{"e.other = 1"}
	return strings.Join(where, " AND ")
}
`
	fset := token.NewFileSet()
	f := mustParseFile(t, fset, "fixture.go", src)
	var buildFn, buildNoTenantFn *ast.FuncDecl
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			switch fn.Name.Name {
			case "build":
				buildFn = fn
			case "buildNoTenant":
				buildNoTenantFn = fn
			}
		}
	}
	if !localVarContainsTenantLiteral(buildFn, "where") {
		t.Error("build's `where` should be credited with tenant evidence")
	}
	if localVarContainsTenantLiteral(buildNoTenantFn, "where") {
		t.Error("buildNoTenant's `where` should not be credited with tenant evidence")
	}
}
