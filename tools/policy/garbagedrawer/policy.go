// Package garbagedrawer implements ARCH-GO-017, the architectural package
// naming and API-boundary check. It keeps package ownership visible in the
// import path and rejects names which encourage unrelated behavior to collect
// behind a convenient, unowned drawer.
package garbagedrawer

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// PolicyDate is the deterministic date used when evaluating reviewed
// exceptions. It deliberately does not use time.Now: a policy result should
// not change between two runs of the same checkout.
const PolicyDate = "2026-09-03"

// Symbol describes an exported declaration in a package. Kind is optional
// and is retained for diagnostics (for example, "func" or "type").
type Symbol struct {
	Name string
	Kind string
	Doc  string
}

// Package is the source-level information needed by the checker. Callers can
// construct it directly for tests; ScanPackages fills it from go list and the
// Go AST.
type Package struct {
	ImportPath      string
	Dir             string
	Name            string
	PackageDoc      string
	ExportedSymbols []Symbol
	Generated       bool
}

// Violation is one architectural finding. Kind is stable and suitable for
// machine filtering; Message is intended for a human reviewing the package.
type Violation struct {
	ImportPath  string
	PackageName string
	Kind        string
	Symbol      string
	Message     string
	Waived      bool
}

func (v Violation) String() string {
	if v.Symbol != "" {
		return fmt.Sprintf("%s: %s (%s: %s)", v.ImportPath, v.Message, v.Kind, v.Symbol)
	}
	return fmt.Sprintf("%s: %s (%s)", v.ImportPath, v.Message, v.Kind)
}

// Exception is a narrow, reviewed and expiring exception to ARCH-GO-017.
// ImportPath may be exact or a package-prefix (ending in "/"). Every field
// is required when an exception is used so an exception cannot become a
// permanent undocumented escape hatch.
type Exception struct {
	ImportPath      string `yaml:"import_path" json:"import_path"`
	Path            string `yaml:"path" json:"path"`
	PathPrefix      string `yaml:"path_prefix" json:"path_prefix"`
	Kind            string `yaml:"kind" json:"kind"`
	Owner           string `yaml:"owner" json:"owner"`
	Rationale       string `yaml:"rationale" json:"rationale"`
	ReplacementPlan string `yaml:"replacement_plan" json:"replacement_plan"`
	FollowupTodo    string `yaml:"followup_todo" json:"followup_todo"`
	Expiry          string `yaml:"expiry" json:"expiry"`
}

// Policy contains reviewed exceptions. ApprovedPackages is an explicit
// semantic-owner declaration for an otherwise generic API; it is also
// expiring and reviewed, so approvals cannot silently broaden the policy.
type Policy struct {
	Exceptions       []Exception `yaml:"exceptions" json:"exceptions"`
	ApprovedPackages []Exception `yaml:"approved_packages" json:"approved_packages"`
	PolicyDate       string      `yaml:"policy_date" json:"policy_date"`
}

// DefaultPolicy returns the current deterministic policy with no exceptions.
func DefaultPolicy() Policy { return Policy{PolicyDate: PolicyDate} }

// Load reads a YAML policy document. A missing exceptions or
// approved_packages section is valid and results in an empty list.
func Load(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("garbagedrawer: reading policy: %w", err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("garbagedrawer: parsing policy: %w", err)
	}
	if p.PolicyDate == "" {
		p.PolicyDate = PolicyDate
	}
	return p, nil
}

// Active reports whether an exception is valid at date. ISO dates sort
// lexicographically; malformed or empty dates are never active.
func (e Exception) Active(date string) bool {
	return validDate(e.Expiry) && validDate(date) && e.Expiry >= date
}

func validDate(s string) bool {
	if len(s) != len("2006-01-02") {
		return false
	}
	for i, r := range s {
		if i == 4 || i == 7 {
			if r != '-' {
				return false
			}
		} else if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (e Exception) Covers(v Violation) bool {
	path := e.ImportPath
	if path == "" {
		path = e.Path
	}
	if path == "" {
		path = e.PathPrefix
	}
	if path == "" || e.Kind == "" {
		return false
	}
	if e.PathPrefix != "" {
		if !strings.HasPrefix(v.ImportPath, strings.TrimSuffix(e.PathPrefix, "/")+"/") {
			return false
		}
	} else if path != v.ImportPath && !strings.HasPrefix(v.ImportPath, strings.TrimSuffix(path, "/")+"/") {
		return false
	}
	return e.Kind == v.Kind || e.Kind == "*"
}

// CheckPackage returns raw findings and never hides them behind an exception.
// Integration checks can use CheckPackages to apply explicit reviewed
// exceptions while primary/golden tests continue to exercise the raw rule.
func CheckPackage(pkg Package) []Violation {
	if pkg.Generated || pkg.ImportPath == "" {
		return nil
	}
	parts := strings.Split(strings.Trim(pkg.ImportPath, "/"), "/")
	name := pkg.Name
	if name == "" && len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	var out []Violation
	seenName := map[string]bool{}
	checkName := func(part string) {
		lower := strings.ToLower(part)
		switch lower {
		case "services", "utils", "helpers", "common", "managers":
			if seenName["garbage_drawer_name"] {
				return
			}
			seenName["garbage_drawer_name"] = true
			out = append(out, Violation{ImportPath: pkg.ImportPath, PackageName: name, Kind: "garbage_drawer_name", Message: fmt.Sprintf("package path segment %q is a garbage-drawer name without a single semantic owner", part)})
		}
		if lower == "impl" || strings.HasSuffix(lower, "impl") {
			if seenName["impl_suffix"] {
				return
			}
			seenName["impl_suffix"] = true
			out = append(out, Violation{ImportPath: pkg.ImportPath, PackageName: name, Kind: "impl_suffix", Message: fmt.Sprintf("package path segment %q hides implementation behind an impl package", part)})
		}
	}
	checkName(name)
	for _, part := range parts {
		checkName(part)
	}
	// Central registries of models/repositories are specifically forbidden;
	// domain-local singular model packages (for example internal/intent/model)
	// remain valid semantic owners.
	if isCentralDrawer(parts) {
		out = append(out, Violation{ImportPath: pkg.ImportPath, PackageName: name, Kind: "central_models_or_repositories", Message: "central models/repositories package has no bounded semantic owner"})
	}
	if isCatchAll(pkg) {
		out = append(out, Violation{ImportPath: pkg.ImportPath, PackageName: name, Kind: "catch_all_exported_api", Message: "exported API is composed only of generic operations and has no bounded semantic meaning"})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

func isCentralDrawer(parts []string) bool {
	for i, p := range parts {
		if strings.EqualFold(p, "internal") && i+1 < len(parts) && (strings.EqualFold(parts[i+1], "models") || strings.EqualFold(parts[i+1], "repositories") || strings.EqualFold(parts[i+1], "repository")) {
			return true
		}
		if i == 0 && (strings.EqualFold(p, "models") || strings.EqualFold(p, "repositories") || strings.EqualFold(p, "repository")) {
			return true
		}
	}
	return false
}

var genericSymbols = map[string]bool{
	"New": true, "Get": true, "Set": true, "Do": true, "Run": true,
	"Handle": true, "Execute": true, "Process": true, "Manager": true,
	"Service": true, "Repository": true, "Model": true, "Helper": true,
	"Util": true, "Common": true, "Config": true, "Options": true,
	"Client": true, "Store": true,
}

func isCatchAll(pkg Package) bool {
	if len(pkg.ExportedSymbols) < 2 || approvedSemanticDoc(pkg.PackageDoc) {
		return false
	}
	for _, s := range pkg.ExportedSymbols {
		if !genericSymbols[s.Name] {
			return false
		}
	}
	// A package with an explicitly semantic package comment is bounded even
	// when its API has conventional constructors/configuration symbols. A bare
	// "Package foo" comment is intentionally not enough: ownership must be
	// stated in terms of a bounded domain, engine, adapter, or protocol.
	return true
}

func approvedSemanticDoc(doc string) bool {
	if strings.TrimSpace(doc) == "" {
		return false
	}
	lower := strings.ToLower(doc)
	for _, marker := range []string{"owns ", "implements ", "bounded ", "semantic owner", "adapter", "domain", "engine", "protocol", "policy", "parser", "transport"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// CheckPackages evaluates packages, suppressing only findings covered by a
// fully populated active exception. Invalid/expired exceptions never waive a
// finding. Findings are sorted for deterministic CI output.
func CheckPackages(packages []Package, policy Policy) []Violation {
	date := policy.PolicyDate
	if date == "" {
		date = PolicyDate
	}
	var out []Violation
	for _, pkg := range packages {
		for _, v := range CheckPackage(pkg) {
			waived := false
			for _, e := range append(append([]Exception{}, policy.Exceptions...), policy.ApprovedPackages...) {
				if e.Owner != "" && e.Rationale != "" && (e.ReplacementPlan != "" || e.FollowupTodo != "") && e.Active(date) && e.Covers(v) {
					waived = true
					break
				}
			}
			if !waived {
				out = append(out, v)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Scan returns raw ARCH-GO-017 findings from the root module's real Go tree.
func Scan(root string) ([]Violation, error) {
	return ScanWithPolicy(root, DefaultPolicy())
}

// ScanWithPolicy scans and evaluates the real tree with policy exceptions.
func ScanWithPolicy(root string, policy Policy) ([]Violation, error) {
	packages, err := ScanPackages(root)
	if err != nil {
		return nil, err
	}
	return CheckPackages(packages, policy), nil
}

type listPackage struct {
	ImportPath string
	Dir        string
	Name       string
	GoFiles    []string
	CgoFiles   []string
}

// ScanPackages obtains the package graph from go list, then reads only the
// package's own Go files. Generated code and node-managed source trees are
// outside this source-ownership policy.
func ScanPackages(root string) ([]Package, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("garbagedrawer: resolve root: %w", err)
	}
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = absoluteRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("garbagedrawer: go list: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var result []Package
	for dec.More() {
		var lp listPackage
		if err := dec.Decode(&lp); err != nil {
			return nil, fmt.Errorf("garbagedrawer: decode go list: %w", err)
		}
		relDir, relErr := filepath.Rel(absoluteRoot, lp.Dir)
		if relErr != nil {
			return nil, fmt.Errorf("garbagedrawer: package %s path: %w", lp.ImportPath, relErr)
		}
		relDir = filepath.ToSlash(relDir)
		generated := relDir == "gen" || strings.HasPrefix(relDir, "gen/") || relDir == "node_modules" || strings.HasPrefix(relDir, "node_modules/")
		p := Package{ImportPath: lp.ImportPath, Dir: lp.Dir, Name: lp.Name, Generated: generated}
		if !generated {
			files := append(append([]string{}, lp.GoFiles...), lp.CgoFiles...)
			for _, file := range files {
				path := filepath.Join(lp.Dir, file)
				f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
				if err != nil {
					return nil, fmt.Errorf("garbagedrawer: parse %s: %w", path, err)
				}
				if p.PackageDoc == "" && f.Doc != nil {
					p.PackageDoc = f.Doc.Text()
				}
				for _, decl := range f.Decls {
					var sym Symbol
					switch d := decl.(type) {
					case *ast.FuncDecl:
						if d.Name.IsExported() {
							sym = Symbol{Name: d.Name.Name, Kind: "func", Doc: docText(d.Doc)}
						}
					case *ast.GenDecl:
						for _, spec := range d.Specs {
							if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
								p.ExportedSymbols = append(p.ExportedSymbols, Symbol{Name: ts.Name.Name, Kind: "type", Doc: docText(ts.Doc)})
							}
						}
					}
					if sym.Name != "" {
						p.ExportedSymbols = append(p.ExportedSymbols, sym)
					}
				}
			}
		}
		result = append(result, p)
	}
	return result, nil
}

func docText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return g.Text()
}

// Keep unicode in the implementation contract: package names are Go
// identifiers, and this helper is useful to callers validating fixture names.
func IsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if i > 0 && !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}
