package application

// TestCompositionRootRejectsGlobalRegistrationAndHiddenDependencies is
// ARCH-GO-020's PRIMARY test.
//
// It is a static scan of the real tree rather than an assertion about this
// package, because the property it defends is a property of the repository:
// "there is exactly one place where this application is assembled". A test
// that only inspected internal/application would pass happily while a domain
// package quietly registered itself at init, or a command grew its own
// business wiring, which is the failure the todo names.
//
// Five rules, each reported with the file, the line and the rule that names
// it:
//
//   - init-registration: no production package under internal/ or cmd/ may do
//     anything in a func init(). Registration that happens because a package
//     was linked in is registration nobody chose.
//   - package-level-mutable-registry: no package-level map, slice or sync.Map
//     under internal/ or cmd/ that anything in its own package writes to
//     after declaration. The rule is deliberately about mutation, not about
//     shape: a package-level lookup table that is only ever read is a
//     constant with a map literal, while one something registers into is
//     shared state every caller inherits and none of them composed.
//   - composition-root-package-state: internal/application itself declares no
//     package-level state beyond sentinel errors and interface assertions,
//     and exposes no name-keyed lookup returning any. A composition root with
//     a lookup table is a service locator.
//   - business-hidden-dependency: a business package may not reach the
//     process environment, the network or a database driver itself. Those are
//     dependencies its callers cannot see, substitute or observe.
//   - command-business-semantics: a cmd/ package may not import a business
//     layer or the application cell directly. A command parses configuration,
//     selects a role and invokes a lifecycle.
//
// Exceptions are pinned in compositionRootExceptions with the owner who is
// expected to resolve them, so a real, currently-observed condition stays
// visible instead of being hidden by a looser rule. An exception that stops
// matching is itself a failure: the allowlist may not outlive the thing it
// excuses.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Rule identifiers, so a failure names one stable string.
const (
	ruleInitRegistration   = "init-registration"
	ruleMutableRegistry    = "package-level-mutable-registry"
	ruleRootPackageState   = "composition-root-package-state"
	ruleHiddenDependency   = "business-hidden-dependency"
	ruleCommandBusinessSem = "command-business-semantics"
)

const (
	compositionRootPackage  = "internal/application"
	compositionRootModule   = "github.com/monstercameron/hcm-next"
	compositionRootCmdRoot  = "cmd"
	compositionRootIntRoot  = "internal"
	compositionRootTestData = "testdata"
)

// businessRoots are the layers that own HCM meaning: the layers
// definitions/architecture/package-dependency-policy.yaml calls
// business_layers, plus governance, which is business meaning about who may
// act.
var businessRoots = []string{
	"internal/domains",
	"internal/engines",
	"internal/capability",
	"internal/workflow",
	"internal/governance",
}

// commandForbiddenImports are the packages a command must reach through
// internal/application instead of importing itself. Adapter subpackages
// underneath them (internal/intent/app/pgstore, for instance) are not
// business semantics and are not listed: a composition root may name the
// concrete store it composes.
var commandForbiddenImports = []string{
	"internal/domains",
	"internal/engines",
	"internal/capability",
	"internal/workflow",
	"internal/governance",
	"internal/intent/app",
}

// hiddenDependencyCalls are the process-wide dependencies a business package
// must not reach for itself: the environment, the network and a database
// driver. Each is written as the selector a call site spells.
var hiddenDependencyCalls = map[string]string{
	"os.Getenv":          "reads the process environment",
	"os.LookupEnv":       "reads the process environment",
	"os.Setenv":          "writes the process environment",
	"net.Listen":         "binds a socket",
	"net.Dial":           "opens a network connection",
	"sql.Open":           "opens a database",
	"pgxpool.New":        "opens a database pool",
	"http.Get":           "calls out over HTTP",
	"http.Post":          "calls out over HTTP",
	"http.DefaultClient": "calls out over HTTP",
}

// compositionRootException pins one real, reviewed violation that exists in
// the tree today, with the owner expected to resolve it. An exception names a
// file and a rule: a blanket relaxation would make the scan describe the
// policy rather than the tree.
type compositionRootException struct {
	File   string
	Rule   string
	Owner  string
	Reason string
}

// compositionRootExceptions is the reviewed exception set: the package-level
// mutable state that exists in the tree as of this todo.
var compositionRootExceptions = []compositionRootException{
	{
		File:  "internal/domains/pseudonym/revelation.go",
		Rule:  ruleMutableRegistry,
		Owner: "domain-teams (ANON-003/ANON-004)",
		Reason: "revelationUseStates is a package-level sync.Map keyed by *IdentityEscrow, holding the " +
			"one-time-use set that makes a revelation receipt unrepeatable. It is real process-wide state no " +
			"composition root built: two escrows composed independently share one table, and nothing can " +
			"substitute or inspect it. Moving the used-digest set onto the escrow value belongs to the todo " +
			"that owns the escrow.",
	},
	{
		File:  "internal/trust/custody/lifecycle.go",
		Rule:  ruleMutableRegistry,
		Owner: "governance-and-trust (TRUST-015)",
		Reason: "lifecycleStores is the same shape: a package-level sync.Map keyed by *InMemoryFake, holding " +
			"the fake custody provider's key lifecycle records. It is confined to the in-memory fake rather " +
			"than to a production adapter, which is why it is excused rather than fixed here, but it is still " +
			"state a caller inherits instead of composing.",
	},
}

// violation is one finding.
type violation struct {
	File   string
	Line   int
	Rule   string
	Detail string
}

func (v violation) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", v.File, v.Line, v.Rule, v.Detail)
}

// sourcePackage is one production package: its module-relative directory and
// its parsed non-test files.
type sourcePackage struct {
	Dir   string
	Files map[string]*ast.File
}

func TestCompositionRootRejectsGlobalRegistrationAndHiddenDependencies(t *testing.T) {
	root := repositoryRoot(t)
	fset := token.NewFileSet()
	packages := loadProductionPackages(t, fset, root)
	if len(packages) == 0 {
		t.Fatal("the scan read no production packages; it would pass whatever the tree contained")
	}

	files := 0
	var found []violation
	for _, pkg := range packages {
		files += len(pkg.Files)
		found = append(found, scanPackage(fset, pkg)...)
	}
	t.Logf("scanned %d production files in %d packages under internal/ and cmd/", files, len(packages))

	allowed := map[string]compositionRootException{}
	for _, exception := range compositionRootExceptions {
		if exception.Owner == "" || exception.Reason == "" {
			t.Errorf("exception for %s/%s names no owner or no reason", exception.File, exception.Rule)
		}
		allowed[exception.File+"|"+exception.Rule] = exception
	}

	var unexcused []violation
	used := map[string]bool{}
	for _, v := range found {
		key := v.File + "|" + v.Rule
		if exception, ok := allowed[key]; ok {
			used[key] = true
			t.Logf("allowed exception: %s (owner %s)", v, exception.Owner)
			continue
		}
		unexcused = append(unexcused, v)
	}
	for key, exception := range allowed {
		if !used[key] {
			t.Errorf("exception %s/%s (owner %s) no longer matches anything; remove it",
				exception.File, exception.Rule, exception.Owner)
		}
	}

	sort.Slice(unexcused, func(i, j int) bool { return unexcused[i].String() < unexcused[j].String() })
	for _, v := range unexcused {
		t.Errorf("%s", v)
	}
}

// loadProductionPackages parses every non-test Go file under internal/ and
// cmd/, grouped by directory.
func loadProductionPackages(t *testing.T, fset *token.FileSet, root string) []sourcePackage {
	t.Helper()
	byDir := map[string]map[string]*ast.File{}
	for _, top := range []string{compositionRootIntRoot, compositionRootCmdRoot} {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(pathName string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == compositionRootTestData {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, pathName)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			parsed, parseErr := parser.ParseFile(fset, pathName, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", rel, parseErr)
			}
			dir := path.Dir(rel)
			if byDir[dir] == nil {
				byDir[dir] = map[string]*ast.File{}
			}
			byDir[dir][rel] = parsed
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", top, err)
		}
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	out := make([]sourcePackage, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, sourcePackage{Dir: dir, Files: byDir[dir]})
	}
	return out
}

// scanPackage applies every rule to one package.
func scanPackage(fset *token.FileSet, pkg sourcePackage) []violation {
	var out []violation
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	// The registry rule is a package-level question: a package variable is a
	// registry when something in its own package writes to it after
	// declaration, and that write can live in any file.
	candidates := map[string]violation{}
	for _, name := range names {
		collectRegistryCandidates(fset, pkg.Files[name], name, candidates)
	}
	written := map[string]bool{}
	for _, name := range names {
		collectPackageWrites(pkg.Files[name], written)
	}
	for name, candidate := range candidates {
		if written[name] {
			out = append(out, candidate)
		}
	}

	for _, name := range names {
		out = append(out, scanFile(fset, pkg.Files[name], name, pkg.Dir)...)
	}
	return out
}

// collectRegistryCandidates records every package-level var whose shape could
// hold registrations: a map, a slice, a sync.Map, or one initialised from a
// map or slice literal.
func collectRegistryCandidates(fset *token.FileSet, file *ast.File, rel string, into map[string]violation) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}
		for _, spec := range genDecl.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if name.Name == "_" {
					continue
				}
				if !registryShaped(value, i) {
					continue
				}
				into[name.Name] = violation{
					File: rel, Line: line(fset, name.Pos()), Rule: ruleMutableRegistry,
					Detail: fmt.Sprintf("package-level %q is written to after declaration: shared mutable state every caller inherits and none composed", name.Name),
				}
			}
		}
	}
}

// registryShaped reports whether one name in a var spec has a shape that can
// accumulate registrations.
func registryShaped(value *ast.ValueSpec, index int) bool {
	if collectionType(value.Type) {
		return true
	}
	if index < len(value.Values) {
		return collectionValue(value.Values[index])
	}
	if len(value.Values) == 1 && len(value.Names) > 1 {
		return collectionValue(value.Values[0])
	}
	return false
}

func collectionType(expr ast.Expr) bool {
	switch node := expr.(type) {
	case *ast.MapType, *ast.ArrayType:
		return true
	case *ast.SelectorExpr:
		ident, ok := node.X.(*ast.Ident)
		return ok && ident.Name == "sync" && node.Sel.Name == "Map"
	}
	return false
}

func collectionValue(expr ast.Expr) bool {
	switch node := expr.(type) {
	case *ast.CompositeLit:
		return collectionType(node.Type)
	case *ast.CallExpr:
		ident, ok := node.Fun.(*ast.Ident)
		if !ok || ident.Name != "make" || len(node.Args) == 0 {
			return false
		}
		return collectionType(node.Args[0])
	}
	return false
}

// collectPackageWrites records every identifier the package assigns to,
// indexes into for assignment, deletes from, takes the address of, or calls a
// sync.Map mutator on.
func collectPackageWrites(file *ast.File, into map[string]bool) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				markWrite(lhs, into)
			}
		case *ast.IncDecStmt:
			markWrite(node.X, into)
		case *ast.UnaryExpr:
			if node.Op == token.AND {
				markWrite(node.X, into)
			}
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok && ident.Name == "delete" && len(node.Args) > 0 {
				markWrite(node.Args[0], into)
			}
			if selector, ok := node.Fun.(*ast.SelectorExpr); ok {
				switch selector.Sel.Name {
				case "Store", "Delete", "LoadOrStore", "Swap", "CompareAndSwap", "Do":
					markWrite(selector.X, into)
				}
			}
		}
		return true
	})
}

// markWrite records the root identifier of an assignable expression.
func markWrite(expr ast.Expr, into map[string]bool) {
	switch node := expr.(type) {
	case *ast.Ident:
		into[node.Name] = true
	case *ast.IndexExpr:
		markWrite(node.X, into)
	case *ast.SelectorExpr:
		markWrite(node.X, into)
	case *ast.StarExpr:
		markWrite(node.X, into)
	}
}

// scanFile applies the per-file rules.
func scanFile(fset *token.FileSet, file *ast.File, rel, pkgDir string) []violation {
	var out []violation
	imports := fileImports(file)

	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			if node.Recv == nil && node.Name.Name == "init" {
				out = append(out, violation{
					File: rel, Line: line(fset, node.Pos()), Rule: ruleInitRegistration,
					Detail: "func init() runs before any composition root decides anything",
				})
			}
			if pkgDir == compositionRootPackage && node.Recv == nil {
				if detail, ok := serviceLocatorSignature(node); ok {
					out = append(out, violation{
						File: rel, Line: line(fset, node.Pos()), Rule: ruleRootPackageState, Detail: detail,
					})
				}
			}
			if isBusinessPackage(pkgDir) {
				out = append(out, scanHiddenDependencies(fset, node, rel, imports)...)
			}
		case *ast.GenDecl:
			if node.Tok != token.VAR || pkgDir != compositionRootPackage {
				continue
			}
			for _, spec := range node.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range value.Names {
					if name.Name == "_" || isSentinelError(value, name.Name) {
						continue
					}
					out = append(out, violation{
						File: rel, Line: line(fset, name.Pos()), Rule: ruleRootPackageState,
						Detail: fmt.Sprintf("the composition root holds package-level %q; composition state belongs in a value a caller passes", name.Name),
					})
				}
			}
		}
	}

	if strings.HasPrefix(rel, compositionRootCmdRoot+"/") {
		for importPath, position := range imports {
			for _, forbidden := range commandForbiddenImports {
				if importPath == compositionRootModule+"/"+forbidden {
					out = append(out, violation{
						File: rel, Line: line(fset, position), Rule: ruleCommandBusinessSem,
						Detail: "a command imports " + forbidden + " directly; reach it through " + compositionRootPackage,
					})
				}
			}
		}
	}
	return out
}

// isSentinelError reports a package-level var that is a declared error value
// rather than composition state.
func isSentinelError(value *ast.ValueSpec, name string) bool {
	if strings.HasPrefix(name, "Err") || strings.HasPrefix(name, "err") {
		return true
	}
	if ident, ok := value.Type.(*ast.Ident); ok && ident.Name == "error" {
		return true
	}
	return false
}

// serviceLocatorSignature reports a function that resolves a dependency by
// name and hands back an untyped value, which is what a service locator is.
func serviceLocatorSignature(node *ast.FuncDecl) (string, bool) {
	if node.Type.Params == nil || node.Type.Results == nil {
		return "", false
	}
	takesName := false
	for _, param := range node.Type.Params.List {
		if ident, ok := param.Type.(*ast.Ident); ok && ident.Name == "string" {
			takesName = true
		}
	}
	if !takesName {
		return "", false
	}
	for _, result := range node.Type.Results.List {
		if ident, ok := result.Type.(*ast.Ident); ok && ident.Name == "any" {
			return node.Name.Name + " resolves a dependency by name and returns any: that is a service locator", true
		}
		if iface, ok := result.Type.(*ast.InterfaceType); ok && (iface.Methods == nil || len(iface.Methods.List) == 0) {
			return node.Name.Name + " resolves a dependency by name and returns interface{}: that is a service locator", true
		}
	}
	return "", false
}

// scanHiddenDependencies reports a business package reaching the environment,
// the network or a database driver for itself.
func scanHiddenDependencies(fset *token.FileSet, node *ast.FuncDecl, rel string, imports map[string]token.Pos) []violation {
	var out []violation
	ast.Inspect(node, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		call := ident.Name + "." + selector.Sel.Name
		reason, forbidden := hiddenDependencyCalls[call]
		if !forbidden {
			return true
		}
		if !importsPackageNamed(imports, ident.Name) {
			// A local value whose name happens to match a package name is not
			// the standard-library call this rule is about.
			return true
		}
		out = append(out, violation{
			File: rel, Line: line(fset, selector.Pos()), Rule: ruleHiddenDependency,
			Detail: fmt.Sprintf("%s %s inside a business package; the composition root supplies that", call, reason),
		})
		return true
	})
	return out
}

// importsPackageNamed reports whether the file imports a package whose last
// path segment is the given identifier.
func importsPackageNamed(imports map[string]token.Pos, name string) bool {
	for importPath := range imports {
		if path.Base(importPath) == name {
			return true
		}
	}
	return false
}

// fileImports maps every imported path to the position of its spec.
func fileImports(file *ast.File) map[string]token.Pos {
	out := make(map[string]token.Pos, len(file.Imports))
	for _, spec := range file.Imports {
		unquoted, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		out[unquoted] = spec.Pos()
	}
	return out
}

func isBusinessPackage(pkgDir string) bool {
	for _, root := range businessRoots {
		if pkgDir == root || strings.HasPrefix(pkgDir, root+"/") {
			return true
		}
	}
	return false
}

func line(fset *token.FileSet, pos token.Pos) int { return fset.Position(pos).Line }

// repositoryRoot walks up from this package to the module root, so the scan
// reads the live tree rather than a fixture of it.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("no go.mod above %s; the scan has no tree to read", dir)
	return ""
}

// TestCompositionRootScanDetectsEachViolation keeps the PRIMARY test from
// being vacuous.
//
// A tree-wide scan that passes proves nothing unless the scan can fail. Each
// case below is a synthetic package containing exactly one of the five
// failures, checked against the same scanner the live scan runs. The last two
// are the shapes this todo actually removed: a command that reached into the
// application cell itself, and a composition root holding its own state.
func TestCompositionRootScanDetectsEachViolation(t *testing.T) {
	cases := []struct {
		name     string
		dir      string
		file     string
		source   string
		wantRule string
	}{
		{
			name: "init-time registration",
			dir:  "internal/domains/example", file: "internal/domains/example/a.go",
			source:   "package example\n\nfunc init() { register() }\nfunc register() {}\n",
			wantRule: ruleInitRegistration,
		},
		{
			name: "a package-level map something registers into",
			dir:  "internal/domains/example", file: "internal/domains/example/a.go",
			source:   "package example\n\nvar handlers = map[string]int{}\n\nfunc Register(k string) { handlers[k] = 1 }\n",
			wantRule: ruleMutableRegistry,
		},
		{
			name: "a business package reading the environment",
			dir:  "internal/domains/example", file: "internal/domains/example/a.go",
			source:   "package example\n\nimport \"os\"\n\nfunc Mode() string { return os.Getenv(\"HCMNEXT_MODE\") }\n",
			wantRule: ruleHiddenDependency,
		},
		{
			name: "a command importing the application cell",
			dir:  "cmd/hcmnext", file: "cmd/hcmnext/main.go",
			source:   "package main\n\nimport \"github.com/monstercameron/hcm-next/internal/intent/app\"\n\nvar _ = app.DiscoveryPath\n",
			wantRule: ruleCommandBusinessSem,
		},
		{
			name: "the composition root holding its own state",
			dir:  compositionRootPackage, file: compositionRootPackage + "/x.go",
			source:   "package application\n\nvar composed *int\n",
			wantRule: ruleRootPackageState,
		},
		{
			name: "the composition root resolving by name",
			dir:  compositionRootPackage, file: compositionRootPackage + "/x.go",
			source:   "package application\n\nfunc Resolve(name string) any { return name }\n",
			wantRule: ruleRootPackageState,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := scanSource(t, tc.dir, tc.file, tc.source)
			if !containsRule(found, tc.wantRule) {
				t.Errorf("the scan reported %v, want a %s violation", found, tc.wantRule)
			}
		})
	}

	// The shapes that must stay clean, so the rules are not simply "reject
	// everything": a read-only lookup table, a sentinel error in the
	// composition root, and a command reaching a store adapter.
	clean := []struct {
		name   string
		dir    string
		file   string
		source string
	}{
		{
			"a read-only lookup table", "internal/domains/example", "internal/domains/example/a.go",
			"package example\n\nvar ranks = map[string]int{\"a\": 1}\n\nfunc Rank(k string) int { return ranks[k] }\n",
		},
		{
			"a sentinel error in the composition root", compositionRootPackage, compositionRootPackage + "/x.go",
			"package application\n\nimport \"errors\"\n\nvar ErrUnknown = errors.New(\"unknown\")\n",
		},
		{
			"a command composing a concrete store", "cmd/migrate", "cmd/migrate/main.go",
			"package main\n\nimport \"github.com/monstercameron/hcm-next/internal/intent/app/pgstore\"\n\nvar _ = pgstore.TenantID\n",
		},
	}
	for _, tc := range clean {
		t.Run(tc.name, func(t *testing.T) {
			if found := scanSource(t, tc.dir, tc.file, tc.source); len(found) != 0 {
				t.Errorf("the scan reported %v on a shape that is allowed", found)
			}
		})
	}
}

// scanSource runs the live scanner over one synthetic file.
func scanSource(t *testing.T, dir, name, source string) []violation {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse the synthetic source: %v", err)
	}
	return scanPackage(fset, sourcePackage{Dir: dir, Files: map[string]*ast.File{name: file}})
}

func containsRule(found []violation, rule string) bool {
	for _, v := range found {
		if v.Rule == rule {
			return true
		}
	}
	return false
}
