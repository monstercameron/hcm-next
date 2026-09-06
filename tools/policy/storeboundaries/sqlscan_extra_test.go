package storeboundaries

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLScan_LiteralsAndEnvironments(t *testing.T) {
	fset := token.NewFileSet()
	file := mustParseFile(t, fset, "env.go", `package fixture
const prefix = "SELECT "
var columns = "id" + ", tenant_id"
func query() { local := "FROM worker"; _ = prefix + columns + local }
`)
	env := packageEnv(map[string]*ast.File{"env.go": file})
	if env["prefix"] != "SELECT " || env["columns"] != "id, tenant_id" {
		t.Fatalf("packageEnv = %v", env)
	}
	for _, tc := range []struct {
		name  string
		expr  ast.Expr
		local map[string]string
		want  string
		ok    bool
	}{
		{"literal", mustParseExpr(t, `"worker"`), nil, "worker", true},
		{"parenthesized", mustParseExpr(t, `("worker")`), nil, "worker", true},
		{"concat", mustParseExpr(t, `"work" + "er"`), nil, "worker", true},
		{"local identifier", mustParseExpr(t, `name`), map[string]string{"name": "worker"}, "worker", true},
		{"unresolved identifier", mustParseExpr(t, `name`), nil, "", false},
		{"non-string expression", mustParseExpr(t, `1`), nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolveLiteralExpr(tc.expr, tc.local)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("resolveLiteralExpr() = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
	pkg := map[string]string{"name": "pkg"}
	local := map[string]string{"name": "local"}
	if got, ok := resolveArg(mustParseExpr(t, `name`), pkg, local); !ok || got != "local" {
		t.Fatalf("resolveArg local precedence = (%q, %v)", got, ok)
	}
	if got, ok := resolveArg(mustParseExpr(t, `unknown`), pkg, local); ok || got != "" {
		t.Fatalf("resolveArg unresolved = (%q, %v)", got, ok)
	}
	if got, ok := resolveArg(mustParseExpr(t, `"a" + name`), pkg, nil); !ok || got != "apkg" {
		t.Fatalf("resolveArg package concat = (%q, %v)", got, ok)
	}
	fnFile := mustParseFile(t, fset, "fn.go", `package fixture; func build() { one := "SELECT "; two := one + "* FROM worker"; _ = two }`)
	var fn *ast.FuncDecl
	for _, decl := range fnFile.Decls {
		if candidate, ok := decl.(*ast.FuncDecl); ok {
			fn = candidate
		}
	}
	gotLocal := localEnv(fn, map[string]string{"one": "SELECT "})
	if gotLocal["one"] != "SELECT " || gotLocal["two"] != "SELECT * FROM worker" {
		t.Fatalf("localEnv = %v", gotLocal)
	}
}

func TestSQLScan_ExpressionAndStatementHelpers(t *testing.T) {
	if !isFmtSprintf(&ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("fmt"), Sel: ast.NewIdent("Sprintf")}}) {
		t.Fatal("fmt.Sprintf was not recognized")
	}
	if isFmtSprintf(&ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("other"), Sel: ast.NewIdent("Sprintf")}}) || isFmtSprintf(&ast.CallExpr{Fun: ast.NewIdent("Sprintf")}) {
		t.Fatal("non-fmt Sprintf was recognized")
	}
	chain := mustParseExpr(t, `"SELECT " + "*" + " FROM worker"`)
	if leaves := flattenConcat(chain); len(leaves) != 3 {
		t.Fatalf("flattenConcat leaves = %d, want 3", len(leaves))
	}
	if leaves := flattenConcat(mustParseExpr(t, `"SELECT 1"`)); len(leaves) != 1 {
		t.Fatalf("flattenConcat literal leaves = %d", len(leaves))
	}
	format := "SELECT * FROM %s WHERE id = %s %%v"
	args := []ast.Expr{mustParseExpr(t, `"worker"`), mustParseExpr(t, `table`)}
	got, gotArgs := substitutePercentS(format, args, map[string]string{}, nil)
	if got != "SELECT * FROM worker WHERE id = "+dynamicPlaceholder+" %%v" || len(gotArgs) != 2 {
		t.Fatalf("substitutePercentS = (%q, %d args)", got, len(gotArgs))
	}
	if recvTypeName(&ast.StarExpr{X: ast.NewIdent("Store")}) != "Store" || recvTypeName(ast.NewIdent("Store")) != "Store" || recvTypeName(&ast.ArrayType{}) != "?" {
		t.Fatal("recvTypeName receiver handling is incorrect")
	}
}

func TestSQLScan_ParseNonTestFilesAndErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.go"), []byte("package fixture\nconst Name = \"worker\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files, err := parseNonTestGoFiles(fset, dir, []string{"good.go"})
	if err != nil || len(files) != 1 || files["good.go"] == nil {
		t.Fatalf("parseNonTestGoFiles = %v, err=%v", files, err)
	}
	if _, err := parseNonTestGoFiles(fset, dir, []string{"missing.go"}); err == nil {
		t.Fatal("parseNonTestGoFiles accepted a missing file")
	}
	if _, err := parseNonTestGoFiles(fset, dir, []string{"bad.go"}); err == nil {
		t.Fatal("parseNonTestGoFiles accepted an absent invalid file")
	}
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package fixture\nfunc {"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseNonTestGoFiles(fset, dir, []string{"bad.go"}); err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("invalid Go source error = %v", err)
	}
}
