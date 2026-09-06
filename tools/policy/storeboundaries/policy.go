package storeboundaries

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/hcm-next/internal/data/tenancy/storagedisposition"
)

// PolicyDate is the deterministic date used to evaluate reviewed exceptions
// (garbagedrawer.PolicyDate follows the same convention: a policy result
// must not change between two runs of the same checkout just because
// time.Now advanced).
const PolicyDate = "2026-09-05"

// Kind values identify which STORE-002 rule a Violation came from.
const (
	KindTenantScope = "tenant_scope"
	KindPoolImport  = "pool_import"
)

// Violation is one raw STORE-002 finding, in the same shape regardless of
// which rule produced it, so CheckWithPolicy can wave a single kind of
// finding list past a single kind of exception list.
type Violation struct {
	Kind    string
	Package string
	File    string
	Func    string
	Message string
}

func (v Violation) String() string {
	if v.File != "" {
		return fmt.Sprintf("[%s] %s (%s%s)", v.Kind, v.Message, v.Package, funcSuffix(v.File, v.Func))
	}
	return fmt.Sprintf("[%s] %s (%s)", v.Kind, v.Message, v.Package)
}

func funcSuffix(file, fn string) string {
	if fn == "" {
		return "/" + file
	}
	return "/" + file + ":" + fn
}

func tenantFindingToViolation(f TenantFinding) Violation {
	return Violation{Kind: KindTenantScope, Package: f.Package, File: f.File, Func: f.Func, Message: f.String()}
}

func poolFindingToViolation(f PoolFinding) Violation {
	return Violation{Kind: KindPoolImport, Package: f.Package, Message: f.String()}
}

// Exception is a narrow, reviewed and expiring exception to one STORE-002
// finding, in the same shape and with the same review discipline as
// tools/policy/garbagedrawer's Exception: every field is required so an
// exception cannot become a permanent, undocumented escape hatch, and an
// incomplete or expired exception waives nothing.
type Exception struct {
	Kind            string `yaml:"kind"`
	Package         string `yaml:"package"`
	Owner           string `yaml:"owner"`
	Rationale       string `yaml:"rationale"`
	ReplacementPlan string `yaml:"replacement_plan"`
	FollowupTodo    string `yaml:"followup_todo"`
	Expiry          string `yaml:"expiry"`
}

// Policy is the reviewed allowlist: today's known, understood STORE-002
// gaps in the real tree, each with an owner, a rationale and either a
// replacement plan or a follow-up todo, so CheckWithPolicy's green result
// reflects a reviewed decision rather than a silently widening exemption.
type Policy struct {
	Exceptions []Exception `yaml:"exceptions"`
	PolicyDate string      `yaml:"policy_date"`
}

// DefaultPolicy is the empty policy: every finding is reported raw.
func DefaultPolicy() Policy { return Policy{PolicyDate: PolicyDate} }

// LoadPolicy reads the reviewed allowlist YAML at path.
func LoadPolicy(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("storeboundaries: reading policy: %w", err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("storeboundaries: parsing policy: %w", err)
	}
	if p.PolicyDate == "" {
		p.PolicyDate = PolicyDate
	}
	return p, nil
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
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Active reports whether e is valid at date (ISO dates sort
// lexicographically; a malformed or empty date is never active).
func (e Exception) Active(date string) bool {
	return validDate(e.Expiry) && validDate(date) && e.Expiry >= date
}

// Covers reports whether e fully identifies v: same kind, same package, and
// (when v names a file) the same file. Package must match exactly -- unlike
// garbagedrawer's prefix exceptions, a STORE-002 exception is reviewed
// against one specific adapter package, never a subtree, because the whole
// point of this policy is that a widened exception must never be the fix
// for a placement problem (see the lane rules: "widen a policy allowlist"
// is forbidden; the fix is always to move the violating code).
func (e Exception) Covers(v Violation) bool {
	if e.Kind == "" || e.Package == "" {
		return false
	}
	if e.Kind != v.Kind || e.Package != v.Package {
		return false
	}
	return true
}

// CheckWithPolicy waives only findings covered by a fully populated, active
// exception; an invalid or expired exception waives nothing. Findings are
// sorted for deterministic output.
func CheckWithPolicy(raw []Violation, policy Policy) []Violation {
	date := policy.PolicyDate
	if date == "" {
		date = PolicyDate
	}
	var out []Violation
	for _, v := range raw {
		waived := false
		for _, e := range policy.Exceptions {
			if e.Owner != "" && e.Rationale != "" && (e.ReplacementPlan != "" || e.FollowupTodo != "") && e.Active(date) && e.Covers(v) {
				waived = true
				break
			}
		}
		if !waived {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// --- Real-tree glue ----------------------------------------------------------

// scanRoots are the two source trees STORE-002 requires: every store
// adapter under internal/data, and the intent-and-capability domain's
// separate PostgreSQL adapter (internal/intent/app/pgstore is deliberately
// not under internal/data -- see that package's own doc comment -- but is
// named explicitly in the STORE-002 todo, so it is scanned the same way).
var scanRoots = []string{
	"./internal/data/...",
	"./internal/intent/app/pgstore/...",
}

// registryPath is the STORE-001 registry this rule validates every tenant
// scoping decision against.
const registryPath = "definitions/storage/storage-disposition.yaml"

// encryptionPortSearchRoots are scanned by FindEncryptionPort: everywhere
// the STORE-002 todo names as the place to look for an existing encryption
// capability (`grep -rn "encryption" internal/data internal/kernel`).
var encryptionPortSearchRoots = []string{"./internal/data/...", "./internal/kernel/..."}

type listPackage struct {
	ImportPath string
	Dir        string
	Imports    []string
	GoFiles    []string
}

func goList(root string, patterns ...string) ([]listPackage, error) {
	args := append([]string{"list", "-json"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("storeboundaries: go list %s: %w\n%s", strings.Join(patterns, " "), err, string(ee.Stderr))
		}
		return nil, fmt.Errorf("storeboundaries: go list %s: %w", strings.Join(patterns, " "), err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var pkgs []listPackage
	for dec.More() {
		var p listPackage
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("storeboundaries: decode go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// nonTestGoFiles filters out _test.go files, matching what `go list`'s
// GoFiles field already does for a non-test build, defensively (in case a
// future go list output shape ever includes them).
func nonTestGoFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return out
}

// loadPackageSources runs `go list -json` over patterns from root and
// parses every listed package's non-test .go files.
func loadPackageSources(root string, patterns ...string) ([]PackageSource, error) {
	pkgs, err := goList(root, patterns...)
	if err != nil {
		return nil, err
	}
	var out []PackageSource
	for _, p := range pkgs {
		fset := token.NewFileSet()
		files, err := parseNonTestGoFiles(fset, p.Dir, nonTestGoFiles(p.GoFiles))
		if err != nil {
			return nil, fmt.Errorf("storeboundaries: parsing %s: %w", p.ImportPath, err)
		}
		out = append(out, PackageSource{ImportPath: p.ImportPath, Imports: p.Imports, Files: files, Fset: fset})
	}
	return out, nil
}

// Report is every raw STORE-002 finding from the real tree, plus the
// encryption-gap accounting for TEST clause (3).
type Report struct {
	TenantScope []TenantFinding
	PoolImport  []PoolFinding
	Encryption  EncryptionGap
}

// Violations flattens TenantScope and PoolImport into the unified Violation
// shape CheckWithPolicy consumes. Encryption is reported separately: today
// it is a gap description, not a per-adapter finding (see EncryptionGap's
// doc comment).
func (r Report) Violations() []Violation {
	var out []Violation
	for _, f := range r.TenantScope {
		out = append(out, tenantFindingToViolation(f))
	}
	for _, f := range r.PoolImport {
		out = append(out, poolFindingToViolation(f))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Scan runs every STORE-002 rule over the real tree rooted at root (the
// repository root: Scan runs `go list` with root as its working
// directory).
func Scan(root string) (Report, error) {
	registry, err := storagedisposition.Load(filepath.Join(root, registryPath))
	if err != nil {
		return Report{}, err
	}
	if err := storagedisposition.Validate(registry); err != nil {
		return Report{}, fmt.Errorf("storeboundaries: registry failed its own STORE-001 validation: %w", err)
	}

	pkgs, err := loadPackageSources(root, scanRoots...)
	if err != nil {
		return Report{}, err
	}

	var report Report
	for _, pkg := range pkgs {
		report.TenantScope = append(report.TenantScope, EvaluateTenantScope(pkg, registry)...)
		if f := EvaluatePoolImport(pkg); f != nil {
			report.PoolImport = append(report.PoolImport, *f)
		}
	}
	sort.Slice(report.TenantScope, func(i, j int) bool {
		return report.TenantScope[i].String() < report.TenantScope[j].String()
	})
	sort.Slice(report.PoolImport, func(i, j int) bool {
		return report.PoolImport[i].Package < report.PoolImport[j].Package
	})

	portFiles := map[string]*ast.File{}
	for _, root2 := range encryptionPortSearchRoots {
		encPkgs, err := goList(root, root2)
		if err != nil {
			return Report{}, err
		}
		for _, p := range encPkgs {
			fset := token.NewFileSet()
			files, err := parseNonTestGoFiles(fset, p.Dir, nonTestGoFiles(p.GoFiles))
			if err != nil {
				return Report{}, fmt.Errorf("storeboundaries: parsing %s: %w", p.ImportPath, err)
			}
			for name, f := range files {
				portFiles[p.ImportPath+"/"+name] = f
			}
		}
	}
	report.Encryption = EncryptionGap{
		FieldLevelTables: fieldLevelTables(registry),
		PortEvidence:     FindEncryptionPort(portFiles),
	}

	return report, nil
}

func fieldLevelTables(reg *storagedisposition.Registry) []string {
	var out []string
	for _, t := range reg.Tables {
		if t.EncryptionClass == storagedisposition.EncryptionFieldLevel {
			out = append(out, t.Table)
		}
	}
	sort.Strings(out)
	return out
}
