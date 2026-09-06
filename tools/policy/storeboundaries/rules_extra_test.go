package storeboundaries

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestRules_IdentifierAndFindingFormatting(t *testing.T) {
	for _, tc := range []struct {
		text, word string
		want       bool
	}{
		{"worker", "worker", true},
		{"worker_history", "worker", false},
		{"x worker y", "worker", true},
		{"", "worker", false},
	} {
		if got := WholeWordContains(tc.text, tc.word); got != tc.want {
			t.Errorf("WholeWordContains(%q, %q) = %v, want %v", tc.text, tc.word, got, tc.want)
		}
	}
	for _, tc := range []struct {
		b    byte
		want bool
	}{
		{'a', true}, {'Z', true}, {'7', true}, {'_', true}, {'-', false}, {' ', false},
	} {
		if got := IsIdentifierByte(tc.b); got != tc.want {
			t.Errorf("IsIdentifierByte(%q) = %v, want %v", tc.b, got, tc.want)
		}
	}
	tenant := TenantFinding{Package: "pkg", File: "store.go", Func: "List", Verb: VerbSelect, Tables: []string{"worker"}}
	if got := tenant.String(); !strings.Contains(got, "store.go: SELECT List") || !strings.Contains(got, "worker") {
		t.Fatalf("TenantFinding.String() = %q", got)
	}
	pool := PoolFinding{Package: "pkg"}
	if got := pool.String(); !strings.Contains(got, "pkg") || !strings.Contains(got, "pgxpool") {
		t.Fatalf("PoolFinding.String() = %q", got)
	}
}

func TestRules_HeuristicEvidence(t *testing.T) {
	src := `package fixture
import "fmt"
func query(td metadata, ex Executor) {
  where := "tenant_id = $1"
  ex.Query(fmt.Sprintf("SELECT * FROM %s WHERE %s", "worker", where))
}
func noQuery(ex Executor) { ex.Query(fmt.Sprintf("SELECT * FROM %s WHERE id = $1", "worker")) }
type metadata struct { TenantScopingColumn string }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var queryFn, noQueryFn *ast.FuncDecl
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			switch fn.Name.Name {
			case "query":
				queryFn = fn
			case "noQuery":
				noQueryFn = fn
			}
		}
	}
	if queryFn == nil || noQueryFn == nil {
		t.Fatal("fixture functions not parsed")
	}
	var queryArgs, noQueryArgs []ast.Expr
	ast.Inspect(queryFn, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) > 1 {
			queryArgs = call.Args[1:]
		}
		return true
	})
	ast.Inspect(noQueryFn, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) > 1 {
			noQueryArgs = call.Args[1:]
		}
		return true
	})
	if !argsMentionTenant(queryFn, fset, queryArgs) {
		t.Fatal("tenant-bearing local argument was not recognized")
	}
	if argsMentionTenant(noQueryFn, fset, noQueryArgs) {
		t.Fatal("non-tenant argument was recognized as tenant evidence")
	}
	if !localVarContainsTenantLiteral(queryFn, "where") || localVarContainsTenantLiteral(noQueryFn, "where") {
		t.Fatal("local variable tenant-literal heuristic is incorrect")
	}
	if !exprHasTenantLiteral(mustParseExpr(t, `"e.tenant_id = $1"`)) || exprHasTenantLiteral(mustParseExpr(t, `"e.row_id = $1"`)) {
		t.Fatal("expression tenant-literal heuristic is incorrect")
	}
	if hasDbportTxParam(fset, map[string]*ast.File{"fixture.go": f}) {
		t.Fatal("fixture unexpectedly declared dbport.Tx")
	}
	txFile, err := parser.ParseFile(fset, "tx.go", `package fixture; func Tx(db dbport.Tx) {}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDbportTxParam(fset, map[string]*ast.File{"tx.go": txFile}) {
		t.Fatal("dbport.Tx parameter was not recognized")
	}
	if !importsTenancy([]string{"x/internal/data/tenancy"}) || importsTenancy([]string{"x/internal/data/tenant"}) {
		t.Fatal("importsTenancy classification is incorrect")
	}
	if got := matchTables("SELECT * FROM worker WHERE id = $1", map[string]bool{"worker": true, "worker_history": true}); len(got) != 1 || got[0] != "worker" {
		t.Fatalf("matchTables = %v", got)
	}
}

func TestRules_EncryptionPortAndConsistency(t *testing.T) {
	fset := token.NewFileSet()
	good, err := parser.ParseFile(fset, "ports.go", `package ports
type Encryptor interface { Encrypt([]byte) ([]byte, error); Decrypt([]byte) ([]byte, error) }
type NotAService interface { Read() }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := parser.ParseFile(fset, "other.go", `package ports; type DecryptPort interface { Decrypt(string) error }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := FindEncryptionPort(map[string]*ast.File{"ports.go": good, "other.go": other})
	if strings.Join(got, ",") != "other.go#DecryptPort,ports.go#Encryptor,ports.go#Encryptor" {
		t.Fatalf("FindEncryptionPort = %v", got)
	}
	if got := FindEncryptionPort(map[string]*ast.File{"ports.go": mustParseFile(t, fset, "plain.go", `package ports; type Plain struct{}`)}); len(got) != 0 {
		t.Fatalf("FindEncryptionPort on plain type = %v", got)
	}
	for _, tc := range []struct {
		name string
		gap  EncryptionGap
		want bool
	}{
		{"empty", EncryptionGap{}, true},
		{"field level without port", EncryptionGap{FieldLevelTables: []string{"worker"}}, false},
		{"field level with port", EncryptionGap{FieldLevelTables: []string{"worker"}, PortEvidence: []string{"ports.go#Encryptor"}}, true},
		{"port without field level", EncryptionGap{PortEvidence: []string{"ports.go#Encryptor"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.gap.Consistent(); got != tc.want {
				t.Fatalf("Consistent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func mustParseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	return expr
}
