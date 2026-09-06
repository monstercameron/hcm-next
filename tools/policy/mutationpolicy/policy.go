// Package mutationpolicy enforces mutation adequacy for authority- and
// correctness-bearing packages (GOV-019).
//
// The policy is deliberately kernel-pure: it reads Go source and the compiled
// todo registry, and the fixture runner invokes the Go toolchain without a
// database or a third-party mutation framework.
package mutationpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const modulePath = "github.com/monstercameron/hcm-next"

// PackagePolicy is one declared authority- or correctness-bearing package.
// Root is module-relative and Owner is constrained by OwnerAllowlist.
type PackagePolicy struct {
	Name         string
	Root         string
	Owner        string
	MinSentinels int
}

// OwnerAllowlist is the reviewed owner vocabulary for the declared table.
var OwnerAllowlist = map[string]bool{
	"governance-and-trust":  true,
	"intent-and-capability": true,
	"workflow-runtime":      true,
	"data-and-ledger":       true,
	"domain-teams":          true,
}

var criticalPackageTable = []PackagePolicy{
	{Name: "trust", Root: "internal/trust", Owner: "governance-and-trust", MinSentinels: 2},
	{Name: "authz", Root: "internal/trust/authz", Owner: "governance-and-trust", MinSentinels: 2},
	{Name: "capability", Root: "internal/capability", Owner: "intent-and-capability", MinSentinels: 2},
	{Name: "intent", Root: "internal/intent", Owner: "intent-and-capability", MinSentinels: 2},
	{Name: "workflow-runtime", Root: "internal/workflow/runtime", Owner: "workflow-runtime", MinSentinels: 2},
	{Name: "ledger", Root: "internal/ledger", Owner: "data-and-ledger", MinSentinels: 2},
	{Name: "tenancy", Root: "internal/domains/tenant", Owner: "domain-teams", MinSentinels: 2},
}

// CriticalPackageTable returns a copy of the reviewed package table.
func CriticalPackageTable() []PackagePolicy {
	out := make([]PackagePolicy, len(criticalPackageTable))
	copy(out, criticalPackageTable)
	return out
}

// CriticalPackages is an alias with a concise name for policy consumers.
func CriticalPackages() []PackagePolicy { return CriticalPackageTable() }

type registryTodo struct {
	ID         string            `json:"id"`
	TestMatrix map[string]string `json:"test_matrix"`
}

// MutationTodo is the registry information needed by this policy.
type MutationTodo struct {
	ID       string
	TestName string
}

// MutationTest records a mutation matrix test found in one package.
type MutationTest struct {
	TodoID    string
	Name      string
	File      string
	Sentinels []string
}

// Gap is a machine-readable mutation-policy finding.
type Gap struct {
	Package string
	Owner   string
	Code    string
	TodoID  string
	Test    string
	Detail  string
}

func (g Gap) String() string {
	parts := []string{"GOV-019", g.Package, "owner=" + g.Owner, g.Code}
	if g.TodoID != "" {
		parts = append(parts, g.TodoID)
	}
	if g.Test != "" {
		parts = append(parts, g.Test)
	}
	if g.Detail != "" {
		parts = append(parts, g.Detail)
	}
	return strings.Join(parts, ": ")
}

// PackageReport is the result for one declared package.
type PackageReport struct {
	Policy        PackagePolicy
	Todos         []MutationTodo
	MutationTests []MutationTest
	Sentinels     []string
}

// Report is the complete policy result.
type Report struct {
	Module   string
	Packages []PackageReport
	Gaps     []Gap
}

// LoadMutationTodos reads the compiled planning registry. Only the MUTATION
// entry is used, so registry changes remain the source of truth for names.
func LoadMutationTodos(path string) ([]MutationTodo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read todo registry %s: %w", path, err)
	}
	var todos []registryTodo
	if err := json.Unmarshal(data, &todos); err != nil {
		return nil, fmt.Errorf("parse todo registry %s: %w", path, err)
	}
	seen := map[string]bool{}
	var out []MutationTodo
	for _, todo := range todos {
		name := todo.TestMatrix["MUTATION"]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, MutationTodo{ID: todo.ID, TestName: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TestName < out[j].TestName })
	return out, nil
}

// Scan evaluates every declared package. Mutation todos are resolved by
// matching registry MUTATION names to actual Go test declarations in that
// exact package; no todo ID is duplicated in a hand-maintained package list.
func Scan(root string) (Report, error) {
	registryPath := filepath.Join(root, "definitions", "planning", "todo-registry.json")
	todos, err := LoadMutationTodos(registryPath)
	if err != nil {
		return Report{}, err
	}
	return ScanWithTodos(root, todos)
}

// ScanWithTodos is the pure, fixture-friendly form of Scan.
func ScanWithTodos(root string, todos []MutationTodo) (Report, error) {
	report := Report{Module: modulePath}
	for _, policy := range criticalPackageTable {
		packageReport, gaps, err := scanDeclaredPackage(root, policy, todos)
		if err != nil {
			return report, err
		}
		report.Packages = append(report.Packages, packageReport)
		report.Gaps = append(report.Gaps, gaps...)
	}
	return report, nil
}

func scanDeclaredPackage(root string, policy PackagePolicy, todos []MutationTodo) (PackageReport, []Gap, error) {
	result := PackageReport{Policy: policy}
	if !OwnerAllowlist[policy.Owner] {
		return result, []Gap{{Package: policy.Root, Owner: policy.Owner, Code: "OWNER_NOT_ALLOWLISTED"}}, nil
	}
	dir := filepath.Join(root, filepath.FromSlash(policy.Root))
	files, err := goFiles(dir)
	if err != nil {
		return result, nil, err
	}
	mutationByName := map[string]MutationTodo{}
	for _, todo := range todos {
		mutationByName[todo.TestName] = todo
	}
	var gaps []Gap
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		tests, sentinels, err := inspectTestFile(file)
		if err != nil {
			return result, nil, err
		}
		for _, test := range tests {
			todo, ok := mutationByName[test]
			if !ok {
				continue
			}
			result.Todos = append(result.Todos, todo)
			result.MutationTests = append(result.MutationTests, MutationTest{TodoID: todo.ID, Name: test, File: file, Sentinels: sentinels})
			result.Sentinels = appendUnique(result.Sentinels, sentinels...)
		}
	}
	if len(result.Todos) == 0 {
		gaps = append(gaps, Gap{Package: policy.Root, Owner: policy.Owner, Code: "MISSING_MUTATION_TEST", Detail: "no registry MUTATION test resolves to this package"})
		return result, gaps, nil
	}
	for i, test := range result.MutationTests {
		if len(test.Sentinels) < policy.MinSentinels {
			gaps = append(gaps, Gap{Package: policy.Root, Owner: policy.Owner, Code: "INADEQUATE_MUTATION_TEST", TodoID: test.TodoID, Test: test.Name, Detail: fmt.Sprintf("found %d distinct refusal sentinels/negative assertions; need %d", len(test.Sentinels), policy.MinSentinels)})
		}
		result.MutationTests[i].Sentinels = append([]string(nil), test.Sentinels...)
	}
	return result, gaps, nil
}

func goFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" || entry.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func inspectTestFile(path string) ([]string, []string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
			names = append(names, fn.Name.Name)
		}
	}
	return names, refusalSentinels(file), nil
}

var sentinelWords = []string{
	"DENY", "DENIED", "REJECT", "REFUS", "FORBID", "UNAUTH", "STALE", "CONFLICT",
	"MISMATCH", "INVALID", "MISSING", "UNKNOWN", "EXPIRED", "REVOK", "DUPLICATE",
	"NOT_FOUND", "NOTFOUND", "UNSUPPORTED", "ILLEGAL", "ZERO_EFFECT", "NO_EFFECT",
}

func refusalSentinels(file *ast.File) []string {
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				value := strings.Trim(n.Value, "\"`")
				upper := strings.ToUpper(value)
				for _, word := range sentinelWords {
					if strings.Contains(upper, word) {
						add(value)
						break
					}
				}
			}
		case *ast.Ident:
			upper := strings.ToUpper(n.Name)
			if strings.HasPrefix(upper, "ERR") || strings.Contains(upper, "DENY") || strings.Contains(upper, "STALE") || strings.Contains(upper, "CONFLICT") {
				add(n.Name)
			}
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				add("negative-assertion@" + fmt.Sprint(n.Pos()))
			}
		}
		return true
	})
	sort.Strings(out)
	return out
}

func appendUnique(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = true
	}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			dst = append(dst, value)
		}
	}
	sort.Strings(dst)
	return dst
}

// Mutant describes one comparison operator flip in a Go source file.
type Mutant struct {
	File   string
	Line   int
	Column int
	From   string
	To     string
}

// MutationReport is the result of running the fixture's mutation test.
type MutationReport struct {
	Mutants  []Mutant
	Killed   []Mutant
	Survived []Mutant
}

// ComparisonMutants returns one AST-derived mutant for each comparison in
// source. It flips only semantic comparison operators, one at a time.
func ComparisonMutants(filename string, source []byte) ([]Mutant, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	var mutants []Mutant
	ast.Inspect(file, func(node ast.Node) bool {
		expr, ok := node.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		to, ok := flippedOperator(expr.Op)
		if !ok {
			return true
		}
		position := fset.Position(expr.OpPos)
		mutants = append(mutants, Mutant{File: filename, Line: position.Line, Column: position.Column, From: expr.Op.String(), To: to.String()})
		return true
	})
	return mutants, nil
}

func flippedOperator(op token.Token) (token.Token, bool) {
	switch op {
	case token.EQL:
		return token.NEQ, true
	case token.NEQ:
		return token.EQL, true
	case token.LSS:
		return token.GEQ, true
	case token.GEQ:
		return token.LSS, true
	case token.GTR:
		return token.LEQ, true
	case token.LEQ:
		return token.GTR, true
	default:
		return token.ILLEGAL, false
	}
}

// RunFixture copies a fixture package to a temporary module location, flips
// each comparison operator in its non-test Go source one at a time, and runs
// the named mutation test. A passing test kills that mutant; a passing mutant
// is returned in Survived. The caller can require len(Survived)==0.
func RunFixture(fixtureDir, testName string) (MutationReport, error) {
	return RunFixtureContext(context.Background(), fixtureDir, testName)
}

// RunFixtureContext is RunFixture with cancellation support.
func RunFixtureContext(ctx context.Context, fixtureDir, testName string) (MutationReport, error) {
	if testName == "" {
		return MutationReport{}, errors.New("mutation test name is empty")
	}
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		return MutationReport{}, fmt.Errorf("read fixture %s: %w", fixtureDir, err)
	}
	moduleRoot, err := findModuleRoot(fixtureDir)
	if err != nil {
		return MutationReport{}, err
	}
	tempDir, err := os.MkdirTemp(moduleRoot, ".mutationpolicy-fixture-")
	if err != nil {
		return MutationReport{}, fmt.Errorf("create fixture workspace: %w", err)
	}
	defer os.RemoveAll(tempDir)
	var mutants []Mutant
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(fixtureDir, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			return MutationReport{}, fmt.Errorf("read fixture source %s: %w", path, err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, entry.Name()), source, 0o644); err != nil {
			return MutationReport{}, fmt.Errorf("copy fixture source %s: %w", path, err)
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		fileMutants, err := ComparisonMutants(entry.Name(), source)
		if err != nil {
			return MutationReport{}, err
		}
		mutants = append(mutants, fileMutants...)
	}
	if len(mutants) == 0 {
		return MutationReport{}, errors.New("fixture contains no comparison operators to mutate")
	}
	report := MutationReport{Mutants: append([]Mutant(nil), mutants...)}
	for _, mutant := range mutants {
		if err := copyFixtureSources(fixtureDir, tempDir); err != nil {
			return MutationReport{}, err
		}
		sourcePath := filepath.Join(tempDir, mutant.File)
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			return MutationReport{}, fmt.Errorf("read copied mutant source: %w", err)
		}
		mutated, err := mutateAt(sourcePath, source, mutant.Line, mutant.Column, mutant.From, mutant.To)
		if err != nil {
			return MutationReport{}, fmt.Errorf("mutate %s:%d:%d: %w", mutant.File, mutant.Line, mutant.Column, err)
		}
		if err := os.WriteFile(sourcePath, mutated, 0o644); err != nil {
			return MutationReport{}, fmt.Errorf("write mutant source: %w", err)
		}
		cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-run", "^"+testName+"$")
		cmd.Dir = tempDir
		_, runErr := cmd.CombinedOutput()
		if runErr != nil {
			report.Killed = append(report.Killed, mutant)
			continue
		}
		report.Survived = append(report.Survived, mutant)
	}
	return report, nil
}

func copyFixtureSources(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, entry.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func mutateAt(filename string, source []byte, line, column int, from, to string) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var target *ast.BinaryExpr
	ast.Inspect(file, func(node ast.Node) bool {
		expr, ok := node.(*ast.BinaryExpr)
		if !ok || target != nil {
			return target == nil
		}
		position := fset.Position(expr.OpPos)
		if position.Line == line && position.Column == column && expr.Op.String() == from {
			target = expr
		}
		return target == nil
	})
	if target == nil {
		return nil, fmt.Errorf("comparison at %d:%d not found", line, column)
	}
	toToken := token.Lookup(to)
	if toToken == token.ILLEGAL {
		return nil, fmt.Errorf("unknown replacement operator %q", to)
	}
	target.Op = toToken
	var out bytes.Buffer
	if err := format.Node(&out, fset, file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func findModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above fixture %s", start)
		}
		dir = parent
	}
}
