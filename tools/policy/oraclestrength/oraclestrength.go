// Package oraclestrength enforces GOV-021's minimum assertion quality.
//
// The scanner is deliberately kernel-pure: it parses Go test sources, reads
// the compiled todo registry and delegates seeded mutants to mutationpolicy.
// It never starts a database, network service, or third-party test framework.
package oraclestrength

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/mutationpolicy"
)

const modulePath = "github.com/monstercameron/human-capital-management-suite"

// Strength is the classification assigned to one top-level Go test function.
type Strength string

const (
	WeakOracle   Strength = "WEAK_ORACLE"
	StrongOracle Strength = "STRONG_ORACLE"
)

// TestOracle is the deterministic classification for one test function.
type TestOracle struct {
	Package  string   `json:"package"`
	File     string   `json:"file"`
	Name     string   `json:"name"`
	Line     int      `json:"line"`
	Strength Strength `json:"strength"`
	Reasons  []string `json:"reasons,omitempty"`
}

// Finding is a GOV-021 diagnostic. Class is TEST for the todo's primary test
// and the exact TEST MATRIX key for a secondary test.
type Finding struct {
	Code     string `json:"code"`
	TodoID   string `json:"todo_id,omitempty"`
	Class    string `json:"class,omitempty"`
	TestName string `json:"test_name,omitempty"`
	Package  string `json:"package,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Reason   string `json:"reason"`
}

func (f Finding) String() string {
	parts := []string{"GOV-021", f.Code}
	if f.TodoID != "" {
		parts = append(parts, f.TodoID)
	}
	if f.Class != "" {
		parts = append(parts, f.Class)
	}
	if f.TestName != "" {
		parts = append(parts, f.TestName)
	}
	if f.Package != "" {
		parts = append(parts, f.Package)
	}
	if f.Reason != "" {
		parts = append(parts, f.Reason)
	}
	return strings.Join(parts, ": ")
}

// Report is the result of scanning one root for test functions.
type Report struct {
	Tests    []TestOracle `json:"tests"`
	Findings []Finding    `json:"findings"`
}

// AllowlistEntry records one reviewed exception to registry oracle quality.
// Package may be empty to allow the named test in any package, but a package
// is preferred because test names are not globally unique in Go repositories.
type AllowlistEntry struct {
	TodoID   string `json:"todo_id"`
	Class    string `json:"class"`
	TestName string `json:"test_name"`
	Package  string `json:"package,omitempty"`
	Owner    string `json:"owner"`
	Reason   string `json:"reason"`
}

// RegistryReport is the result of checking every TEST and TEST MATRIX name in
// a compiled todo registry against the scanned test functions.
type RegistryReport struct {
	Oracles     []TestOracle `json:"oracles"`
	AllFindings []Finding    `json:"all_findings"`
	Findings    []Finding    `json:"findings"`
	Allowlisted []Finding    `json:"allowlisted"`
}

// MutationCheck is the GOV-021 seeded-defect result.
type MutationCheck struct {
	TestName string                        `json:"test_name"`
	Report   mutationpolicy.MutationReport `json:"report"`
}

// Scan parses every *_test.go below root and classifies top-level Test, Fuzz,
// and Benchmark functions. Root can be a module, package, or fixture root.
func Scan(root string) (Report, error) {
	if strings.TrimSpace(root) == "" {
		return Report{}, errors.New("oraclestrength: scan root is empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("oraclestrength: resolve root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Report{}, fmt.Errorf("oraclestrength: stat root: %w", err)
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("oraclestrength: root %s is not a directory", root)
	}

	var report Report
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "vendor" || strings.HasPrefix(entry.Name(), ".gocache")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		oracles, err := scanFile(root, path)
		if err != nil {
			return err
		}
		report.Tests = append(report.Tests, oracles...)
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	sort.Slice(report.Tests, func(i, j int) bool { return oracleKey(report.Tests[i]) < oracleKey(report.Tests[j]) })
	for _, oracle := range report.Tests {
		if oracle.Strength == WeakOracle {
			report.Findings = append(report.Findings, Finding{
				Code: "WEAK_ORACLE", TestName: oracle.Name, Package: oracle.Package,
				File: oracle.File, Line: oracle.Line, Reason: strings.Join(oracle.Reasons, ", "),
			})
		}
	}
	return report, nil
}

// ScanPackage is an explicit spelling for callers scanning one package or
// fixture directory.
func ScanPackage(dir string) (Report, error) { return Scan(dir) }

// GoldenJSON returns a stable, location-independent representation useful for
// fixture goldens. Line numbers are intentionally omitted from this format so
// adding a fixture comment does not invalidate a semantic oracle golden.
func GoldenJSON(report Report) ([]byte, error) {
	type entry struct {
		File     string   `json:"file"`
		Name     string   `json:"name"`
		Strength Strength `json:"strength"`
		Reasons  []string `json:"reasons,omitempty"`
	}
	entries := make([]entry, 0, len(report.Tests))
	for _, oracle := range report.Tests {
		entries = append(entries, entry{File: filepath.ToSlash(oracle.File), Name: oracle.Name, Strength: oracle.Strength, Reasons: append([]string(nil), oracle.Reasons...)})
	}
	return json.MarshalIndent(entries, "", "  ")
}

func scanFile(root, path string) ([]TestOracle, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("oraclestrength: parse %s: %w", path, err)
	}
	imports := importAliases(file)
	packageMaps := packageMapVariables(file)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	packagePath := modulePath
	if dir := filepath.Dir(rel); dir != "." && dir != "" {
		packagePath += "/" + filepath.ToSlash(dir)
	}
	var out []TestOracle
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || !isTestFunction(fn.Name.Name) {
			continue
		}
		strength, reasons := classify(fn, imports, mapVariables(fn, packageMaps))
		out = append(out, TestOracle{
			Package: packagePath, File: filepath.ToSlash(rel), Name: fn.Name.Name,
			Line: fset.Position(fn.Pos()).Line, Strength: strength, Reasons: reasons,
		})
	}
	return out, nil
}

func classify(fn *ast.FuncDecl, imports map[string]string, mapNames map[string]bool) (Strength, []string) {
	state := oracleState{}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			state.inspectCall(n, imports, mapNames)
		case *ast.BinaryExpr:
			state.inspectBinary(n)
		case *ast.RangeStmt:
			if isMapExpression(n.X, mapNames) {
				state.mapRange = true
			}
		case *ast.Ident:
			if isStrongWord(n.Name) {
				state.strongField = true
			}
			if strings.Contains(strings.ToLower(n.Name), "golden") || strings.Contains(strings.ToLower(n.Name), "snapshot") {
				state.snapshot = true
			}
		case *ast.SelectorExpr:
			if isStrongWord(n.Sel.Name) {
				state.strongField = true
			}
		}
		return true
	})
	if state.mapRange && state.snapshot {
		state.weakReasons = append(state.weakReasons, "golden or snapshot observes map iteration order")
	}
	if state.unstable {
		state.weakReasons = append(state.weakReasons, "golden or assertion observes time or random data")
	}
	if state.contradictory {
		state.weakReasons = append(state.weakReasons, "assertion accepts contradictory or unrelated alternative outputs")
	}
	if len(state.weakReasons) > 0 {
		return WeakOracle, uniqueSorted(state.weakReasons)
	}
	if !state.assertion {
		return WeakOracle, []string{"test has no recognizable assertion"}
	}
	if !state.strong {
		return WeakOracle, uniqueSorted(append(state.weakReasons, "assertions do not establish an exact result, durable effect, authority, temporal, evidence, or prohibited outcome"))
	}
	return StrongOracle, nil
}

type oracleState struct {
	assertion     bool
	strong        bool
	strongField   bool
	unstable      bool
	snapshot      bool
	contradictory bool
	weakReasons   []string
	mapRange      bool
}

func (s *oracleState) inspectCall(call *ast.CallExpr, imports map[string]string, mapNames map[string]bool) {
	name, receiver := callName(call.Fun)
	lower := strings.ToLower(name + " " + receiver)
	if name == "Now" || name == "Since" || name == "Until" {
		if receiver == "time" || imports[receiver] == "time" {
			s.unstable = true
		}
	}
	if strings.HasPrefix(imports[receiver], "math/rand") || imports[receiver] == "crypto/rand" || receiver == "rand" {
		s.unstable = true
	}
	if strings.Contains(lower, "golden") || strings.Contains(lower, "snapshot") || strings.Contains(lower, "matchsnapshot") {
		s.snapshot = true
		// A map passed directly to a golden/snapshot helper is just as
		// order-sensitive as one ranged in the test body. Treat this as weak
		// even when the helper hides the iteration from the test source.
		for _, arg := range call.Args {
			if isMapExpression(arg, mapNames) {
				s.mapRange = true
				break
			}
		}
	}
	if strings.Contains(lower, "coverage") || strings.Contains(lower, "coverprofile") {
		s.assertion = true
		s.weakReasons = append(s.weakReasons, "assertion checks line or statement coverage")
	}
	if isMockCall(name, receiver) {
		s.assertion = true
		s.weakReasons = append(s.weakReasons, "assertion checks only mock invocation")
	}
	if isWeakAssertionCall(name) {
		s.assertion = true
		s.weakReasons = append(s.weakReasons, weakCallReason(name))
	}
	if isStrongAssertionCall(name) && callContainsStatusOK(call) {
		s.assertion = true
		s.weakReasons = append(s.weakReasons, "assertion checks only HTTP status 200")
	} else if isStrongAssertionCall(name) {
		s.assertion = true
		s.strong = true
	}
	if strings.Contains(lower, "panic") || name == "recover" {
		s.assertion = true
		s.weakReasons = append(s.weakReasons, "assertion checks only that execution does not panic")
	}
	if name == "ErrorIs" || name == "EqualError" || name == "MatchError" || name == "DeepEqual" || ((name == "Is" || name == "As") && (receiver == "errors" || imports[receiver] == "errors")) {
		s.assertion = true
		s.strong = true
	}
}

func (s *oracleState) inspectBinary(expr *ast.BinaryExpr) {
	if expr.Op == token.LOR && comparisonPair(expr) {
		s.assertion = true
		s.contradictory = true
		return
	}
	if expr.Op == token.LAND && contradictoryAndPair(expr) {
		s.assertion = true
		s.contradictory = true
		return
	}
	if !isComparison(expr.Op) {
		return
	}
	s.assertion = true
	if isNilComparison(expr) {
		s.weakReasons = append(s.weakReasons, "assertion checks only nil or non-nil")
		return
	}
	if isStatusOKComparison(expr) {
		s.weakReasons = append(s.weakReasons, "assertion checks only HTTP status 200")
		return
	}
	if isCoverageExpression(expr) {
		s.weakReasons = append(s.weakReasons, "assertion checks line or statement coverage")
		return
	}
	if isMockExpression(expr) {
		s.weakReasons = append(s.weakReasons, "assertion checks only mock invocation")
		return
	}
	if isStrongExpression(expr) || containsStrongNode(expr) {
		s.strong = true
		return
	}
	// A comparison of two named values is the usual exact typed-result oracle.
	if isValueExpression(expr.X) && isValueExpression(expr.Y) {
		s.strong = true
	}
}

func importAliases(file *ast.File) map[string]string {
	aliases := map[string]string{}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, "\"")
		name := filepath.Base(path)
		if spec.Name != nil {
			if spec.Name.Name == "_" {
				continue
			}
			name = spec.Name.Name
		}
		aliases[name] = path
	}
	return aliases
}

func packageMapVariables(file *ast.File) map[string]bool {
	result := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range value.Names {
					isMap := isMapType(value.Type)
					if !isMap && i < len(value.Values) {
						isMap = isMapExpression(value.Values[i], result)
					}
					if isMap && !result[name.Name] {
						result[name.Name], changed = true, true
					}
				}
			}
		}
	}
	return result
}

func mapVariables(fn *ast.FuncDecl, packageMaps map[string]bool) map[string]bool {
	result := make(map[string]bool, len(packageMaps))
	for name := range packageMaps {
		result[name] = true
	}
	// Repeat to resolve alias chains without leaking local names between tests.
	for changed := true; changed; {
		changed = false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.ValueSpec:
				for i, name := range n.Names {
					isMap := isMapType(n.Type)
					if !isMap && i < len(n.Values) {
						isMap = isMapExpression(n.Values[i], result)
					}
					if isMap && !result[name.Name] {
						result[name.Name], changed = true, true
					}
				}
			case *ast.AssignStmt:
				for i, rhs := range n.Rhs {
					if i >= len(n.Lhs) || !isMapExpression(rhs, result) {
						continue
					}
					if ident, ok := n.Lhs[i].(*ast.Ident); ok {
						if !result[ident.Name] {
							result[ident.Name], changed = true, true
						}
					}
				}
			}
			return true
		})
	}
	return result
}

func isMapExpression(expr ast.Expr, mapNames map[string]bool) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return mapNames[ident.Name]
	}
	if literal, ok := expr.(*ast.CompositeLit); ok {
		return isMapType(literal.Type)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	name, _ := callName(call.Fun)
	return name == "make" && len(call.Args) > 0 && isMapType(call.Args[0])
}

func isMapType(expr ast.Expr) bool {
	_, ok := expr.(*ast.MapType)
	return ok
}

func callName(expr ast.Expr) (name, receiver string) {
	switch n := expr.(type) {
	case *ast.Ident:
		return n.Name, ""
	case *ast.SelectorExpr:
		if ident, ok := n.X.(*ast.Ident); ok {
			return n.Sel.Name, ident.Name
		}
		return n.Sel.Name, ""
	default:
		return "", ""
	}
}

func isTestFunction(name string) bool {
	return (strings.HasPrefix(name, "Test") && name != "TestMain") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Benchmark")
}

func isComparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	default:
		return false
	}
}

func isNilComparison(expr *ast.BinaryExpr) bool { return isNil(expr.X) || isNil(expr.Y) }

func isNil(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

func isStatusOKComparison(expr *ast.BinaryExpr) bool {
	return isStatusOK(expr.X) || isStatusOK(expr.Y)
}

func isStatusOK(expr ast.Expr) bool {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.INT {
		return lit.Value == "200"
	}
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		return sel.Sel.Name == "StatusOK"
	}
	return false
}

func callContainsStatusOK(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if isStatusOK(arg) {
			return true
		}
	}
	return false
}

func isCoverageExpression(expr ast.Expr) bool {
	return strings.Contains(strings.ToLower(exprString(expr)), "coverage")
}

func isMockExpression(expr ast.Expr) bool {
	text := strings.ToLower(exprString(expr))
	return strings.Contains(text, "mock") || strings.Contains(text, "invoked") || strings.Contains(text, "called")
}

func isStrongExpression(expr *ast.BinaryExpr) bool {
	return isLenCall(expr.X) || isLenCall(expr.Y) || isProhibited(expr.X) || isProhibited(expr.Y)
}

func isLenCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	name, _ := callName(call.Fun)
	return name == "len"
}

func containsStrongNode(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			found = found || isStrongWord(x.Name) || isProhibited(x)
		case *ast.SelectorExpr:
			found = found || isStrongWord(x.Sel.Name) || isProhibited(x)
		}
		return !found
	})
	return found
}

func isStrongWord(value string) bool {
	lower := strings.ToLower(value)
	for _, word := range []string{"state", "event", "effect", "count", "authority", "principal", "permission", "temporal", "effective", "evidence", "version"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func isProhibited(expr ast.Expr) bool {
	text := strings.ToLower(exprString(expr))
	for _, word := range []string{"denied", "forbidden", "unauthorized", "reject", "rejected", "invalid", "stale", "conflict", "zero_effect", "no_effect"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func isValueExpression(expr ast.Expr) bool {
	switch n := expr.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.SelectorExpr, *ast.IndexExpr, *ast.CallExpr:
		return n != nil
	default:
		return false
	}
}

func isWeakAssertionCall(name string) bool {
	switch strings.ToLower(name) {
	case "notnil", "nil", "notempty", "notzero", "noerror", "assertcalled", "called", "invoked", "coverage":
		return true
	default:
		return false
	}
}

func weakCallReason(name string) string {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "nil") || strings.Contains(lower, "empty") || strings.Contains(lower, "zero") {
		return "assertion checks only nil, non-nil, empty, or zero state"
	}
	if strings.Contains(lower, "coverage") {
		return "assertion checks line or statement coverage"
	}
	return "assertion checks only mock invocation"
}

func isStrongAssertionCall(name string) bool {
	switch strings.ToLower(name) {
	case "equal", "equalerror", "erroris", "matcherror", "deepequal", "elementsmatch", "exactly":
		return true
	default:
		return false
	}
}

func isMockCall(name, receiver string) bool {
	text := strings.ToLower(name + " " + receiver)
	return strings.Contains(text, "mock") || strings.Contains(text, "called") || strings.Contains(text, "invoked") || strings.Contains(text, "expect")
}

func containsComparison(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if expr, ok := n.(*ast.BinaryExpr); ok && isComparison(expr.Op) {
			found = true
		}
		return !found
	})
	return found
}

func comparisonPair(expr *ast.BinaryExpr) bool {
	left, lok := unwrapParen(expr.X).(*ast.BinaryExpr)
	right, rok := unwrapParen(expr.Y).(*ast.BinaryExpr)
	if !lok || !rok || !isComparison(left.Op) || !isComparison(right.Op) {
		return false
	}
	return exprString(left.X) == exprString(right.X) || exprString(left.Y) == exprString(right.Y)
}

// contradictoryAndPair recognizes conjunctions that cannot describe one
// value, without rejecting ordinary range checks such as count > 0 && count <
// 10. A conjunction of two equality claims for the same value (or an equality
// and its negation) is contradictory; inequalities and bounds are not.
func contradictoryAndPair(expr *ast.BinaryExpr) bool {
	left, lok := unwrapParen(expr.X).(*ast.BinaryExpr)
	right, rok := unwrapParen(expr.Y).(*ast.BinaryExpr)
	if !lok || !rok || !isComparison(left.Op) || !isComparison(right.Op) {
		return false
	}
	if (left.Op == token.EQL && right.Op == token.NEQ) || (left.Op == token.NEQ && right.Op == token.EQL) {
		return sameUnorderedPair(left, right)
	}
	if left.Op != token.EQL || right.Op != token.EQL {
		return false
	}
	for _, pair := range [][4]ast.Expr{{left.X, left.Y, right.X, right.Y}, {left.X, left.Y, right.Y, right.X}, {left.Y, left.X, right.X, right.Y}, {left.Y, left.X, right.Y, right.X}} {
		if exprString(pair[0]) == exprString(pair[2]) && distinctLiterals(pair[1], pair[3]) {
			return true
		}
	}
	return false
}

func sameUnorderedPair(left, right *ast.BinaryExpr) bool {
	return (exprString(left.X) == exprString(right.X) && exprString(left.Y) == exprString(right.Y)) ||
		(exprString(left.X) == exprString(right.Y) && exprString(left.Y) == exprString(right.X))
}

func distinctLiterals(left, right ast.Expr) bool {
	l, lok := unwrapParen(left).(*ast.BasicLit)
	r, rok := unwrapParen(right).(*ast.BasicLit)
	return lok && rok && (l.Kind != r.Kind || l.Value != r.Value)
}

func unwrapParen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func exprString(expr ast.Expr) string {
	switch n := expr.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.BasicLit:
		return n.Value
	case *ast.SelectorExpr:
		return exprString(n.X) + "." + n.Sel.Name
	case *ast.CallExpr:
		name, _ := callName(n.Fun)
		return name
	default:
		return ""
	}
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func oracleKey(o TestOracle) string {
	return o.Package + "\x00" + o.File + "\x00" + strconv.Itoa(o.Line) + "\x00" + o.Name
}

// LoadAllowlist reads the JSON reviewed-exception registry. A missing file is
// an error because silently omitting the reviewed file would hide policy drift.
func LoadAllowlist(path string) ([]AllowlistEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oraclestrength: read allowlist %s: %w", path, err)
	}
	var entries []AllowlistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("oraclestrength: parse allowlist %s: %w", path, err)
	}
	return entries, nil
}

// CheckRegistry resolves the registry's TEST and TEST MATRIX names to scanned
// tests and rejects every missing or weak resolution unless allowlisted.
func CheckRegistry(root, registryPath, allowlistPath string) (RegistryReport, error) {
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return RegistryReport{}, fmt.Errorf("oraclestrength: read todo registry: %w", err)
	}
	var todos []todoregistry.Todo
	if err := json.Unmarshal(data, &todos); err != nil {
		return RegistryReport{}, fmt.Errorf("oraclestrength: parse todo registry: %w", err)
	}
	allowlist, err := LoadAllowlist(allowlistPath)
	if err != nil {
		return RegistryReport{}, err
	}
	oracles, err := Scan(root)
	if err != nil {
		return RegistryReport{}, err
	}
	report := RegistryReport{Oracles: oracles.Tests}
	byName := map[string][]TestOracle{}
	for _, oracle := range oracles.Tests {
		byName[oracle.Name] = append(byName[oracle.Name], oracle)
	}
	used := make([]bool, len(allowlist))
	for _, todo := range todos {
		checks := matrixChecks(todo)
		for _, check := range checks {
			matches := byName[check.name]
			if len(matches) == 0 {
				report.addFinding(Finding{Code: "MISSING_STRONG_TEST", TodoID: todo.ID, Class: check.class, TestName: check.name, Reason: "registry test name does not resolve to a Go test function"}, allowlist, used)
				continue
			}
			for _, oracle := range matches {
				if oracle.Strength != StrongOracle {
					report.addFinding(Finding{Code: "WEAK_REGISTRY_ORACLE", TodoID: todo.ID, Class: check.class, TestName: check.name, Package: oracle.Package, File: oracle.File, Line: oracle.Line, Reason: strings.Join(oracle.Reasons, ", ")}, allowlist, used)
				}
			}
		}
	}
	for i, entry := range allowlist {
		if used[i] {
			continue
		}
		report.addFinding(Finding{Code: "STALE_ALLOWLIST", TodoID: entry.TodoID, Class: entry.Class, TestName: entry.TestName, Package: entry.Package, Reason: "allowlist entry does not match a current finding"}, nil, nil)
	}
	sort.Slice(report.AllFindings, func(i, j int) bool { return findingKey(report.AllFindings[i]) < findingKey(report.AllFindings[j]) })
	sort.Slice(report.Findings, func(i, j int) bool { return findingKey(report.Findings[i]) < findingKey(report.Findings[j]) })
	sort.Slice(report.Allowlisted, func(i, j int) bool { return findingKey(report.Allowlisted[i]) < findingKey(report.Allowlisted[j]) })
	return report, nil
}

// ValidateRegistry is a concise compatibility spelling for policy callers.
func ValidateRegistry(root, registryPath, allowlistPath string) (RegistryReport, error) {
	return CheckRegistry(root, registryPath, allowlistPath)
}

type matrixCheck struct{ class, name string }

func matrixChecks(todo todoregistry.Todo) []matrixCheck {
	checks := make([]matrixCheck, 0, len(todo.TestMatrix)+1)
	if todo.Test != "" {
		checks = append(checks, matrixCheck{class: "TEST", name: todo.Test})
	}
	classes := make([]string, 0, len(todo.TestMatrix))
	for class := range todo.TestMatrix {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	seenNames := map[string]bool{}
	if todo.Test != "" {
		seenNames[todo.Test] = true
	}
	for _, class := range classes {
		name := todo.TestMatrix[class]
		if name == "" || (class == "PRIMARY" && seenNames[name]) {
			continue
		}
		seenNames[name] = true
		checks = append(checks, matrixCheck{class: class, name: name})
	}
	return checks
}

func (r *RegistryReport) addFinding(f Finding, entries []AllowlistEntry, used []bool) {
	r.AllFindings = append(r.AllFindings, f)
	for i, entry := range entries {
		if allowlistMatches(entry, f) {
			if used != nil {
				used[i] = true
			}
			r.Allowlisted = append(r.Allowlisted, f)
			return
		}
	}
	r.Findings = append(r.Findings, f)
}

func allowlistMatches(entry AllowlistEntry, finding Finding) bool {
	return entry.TodoID == finding.TodoID && entry.Class == finding.Class && entry.TestName == finding.TestName && (entry.Package == "" || entry.Package == finding.Package)
}

func findingKey(f Finding) string {
	return f.Code + "\x00" + f.TodoID + "\x00" + f.Class + "\x00" + f.TestName + "\x00" + f.Package + "\x00" + f.File
}

// CheckMutation delegates the seeded comparison-operator mutant run to the
// GOV-019 runner. Keeping this adapter thin prevents mutation semantics from
// diverging between policy packages.
func CheckMutation(ctx context.Context, fixtureDir, testName string) (MutationCheck, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	report, err := mutationpolicy.RunFixtureContext(ctx, fixtureDir, testName)
	if err != nil {
		return MutationCheck{}, err
	}
	return MutationCheck{TestName: testName, Report: report}, nil
}

// CheckSeededDefect is the policy-language spelling for CheckMutation.
func CheckSeededDefect(ctx context.Context, fixtureDir, testName string) (MutationCheck, error) {
	return CheckMutation(ctx, fixtureDir, testName)
}
