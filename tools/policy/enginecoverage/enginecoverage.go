// Package enginecoverage implements ENGINE-COVERAGE-001. It keeps reusable
// computation ownership explicit and verifies the source-level wiring of each
// named engine with go/parser rather than relying on a naming convention.
package enginecoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const modulePath = "github.com/monstercameron/human-capital-management-suite"

// Version reports the policy table/scan contract version.
func Version() int { return 1 }

// Explain describes the source-level engine coverage proof.
func Explain() string {
	return "engine coverage v1: drafted-intent computation ownership, ARCH-GO-009 symbols, and parser-proven import wiring"
}

// Responsibility is one reusable computation owned by one engine package for
// one drafted intent. Package is relative to the repository root.
type Responsibility struct {
	Intent      string `json:"intent"`
	Computation string `json:"computation"`
	Package     string `json:"package"`
	Owner       string `json:"owner"`
}

// ImplicitEntry is a reviewed exception for a named computation whose engine
// is not yet imported by an intent definition, app, or conformance package.
// It is exact-key based: a new gap cannot hide behind a broad prefix.
type ImplicitEntry struct {
	Intent      string `json:"intent"`
	Computation string `json:"computation"`
	Package     string `json:"package"`
	Owner       string `json:"owner"`
	Reason      string `json:"reason"`
}

// Finding is one named coverage result.
type Finding struct {
	Kind        string `json:"kind"`
	Intent      string `json:"intent,omitempty"`
	Computation string `json:"computation,omitempty"`
	Package     string `json:"package"`
	Detail      string `json:"detail"`
	Owner       string `json:"owner,omitempty"`
	Allowlisted bool   `json:"allowlisted"`
}

// Report is a deterministic engine coverage scan.
type Report struct {
	Responsibilities []Responsibility `json:"responsibilities"`
	Findings         []Finding        `json:"findings"`
}

// Violations returns findings not covered by the reviewed implicit allowlist.
func (r Report) Violations() []Finding {
	var out []Finding
	for _, finding := range r.Findings {
		if !finding.Allowlisted {
			out = append(out, finding)
		}
	}
	return out
}

// OK reports whether the scan has no new gaps.
func (r Report) OK() bool { return len(r.Violations()) == 0 }

// Digest returns a stable report identity.
func (r Report) Digest() string {
	data, _ := json.Marshal(r)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Options allows fixture tests and future reviewed table revisions without
// changing the repository's default contract.
type Options struct {
	Responsibilities []Responsibility
	Allowlist        []ImplicitEntry
	ImportRoots      []string
}

// Responsibilities is the canonical table for the fourteen drafted intent
// definitions. The table deliberately names computations, not workflow
// steps, so domain workflows cannot silently become reusable-engine owners.
var Responsibilities = []Responsibility{
	{Intent: "hcmnext.people.change_manager", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines"},
	{Intent: "hcmnext.people.change_manager", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines"},
	{Intent: "hcmnext.people.change_manager", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.people.explain_worker_state", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines"},
	{Intent: "hcmnext.people.explain_worker_state", Computation: "transformation", Package: "internal/engines/transformation", Owner: "shared-engines"},
	{Intent: "hcmnext.people.promote_worker", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines"},
	{Intent: "hcmnext.people.promote_worker", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines"},
	{Intent: "hcmnext.people.promote_worker", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines"},
	{Intent: "hcmnext.people.promote_worker", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.people.promote_worker", Computation: "population", Package: "internal/engines/population", Owner: "shared-engines"},
	{Intent: "hcmnext.people.change_manager", Computation: "schedule", Package: "internal/engines/schedule", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.change_base_pay", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.change_base_pay", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.change_base_pay", Computation: "transformation", Package: "internal/engines/transformation", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.simulate_compensation", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.simulate_compensation", Computation: "transformation", Package: "internal/engines/transformation", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.evaluate_pay_band_position", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.evaluate_pay_band_position", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.reserve_compensation_budget", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.reserve_compensation_budget", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.rewards.release_compensation_budget", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines"},
	{Intent: "hcmnext.work.approve_proposal", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.work.approve_proposal", Computation: "transformation", Package: "internal/engines/transformation", Owner: "shared-engines"},
	{Intent: "hcmnext.work.reject_proposal", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.intelligence.explain_transaction", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.intelligence.explain_transaction", Computation: "transformation", Package: "internal/engines/transformation", Owner: "shared-engines"},
	{Intent: "hcmnext.operations.detect_drift", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.operations.detect_drift", Computation: "transformation", Package: "internal/engines/transformation", Owner: "shared-engines"},
	{Intent: "hcmnext.operations.create_repair_plan", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines"},
	{Intent: "hcmnext.operations.create_repair_plan", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.operations.simulate_repair", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines"},
	{Intent: "hcmnext.operations.simulate_repair", Computation: "transformation", Package: "internal/engines/transformation", Owner: "shared-engines"},
}

// DefaultResponsibilities is a defensive copy of the canonical table.
func DefaultResponsibilities() []Responsibility {
	return append([]Responsibility(nil), Responsibilities...)
}

// ImplicitAllowlist records the exact current baseline rows whose reusable
// computation has an owner but is not yet imported by a definition, app, or
// conformance package. A new table row is not covered by this list and makes
// the command fail until it is wired or separately reviewed.
var ImplicitAllowlist = []ImplicitEntry{
	{Intent: "hcmnext.intelligence.explain_transaction", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted provenance intent"},
	{Intent: "hcmnext.operations.create_repair_plan", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines", Reason: "effective-dating consumer wiring is a follow-up to the drafted repair intent"},
	{Intent: "hcmnext.operations.create_repair_plan", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted repair intent"},
	{Intent: "hcmnext.operations.detect_drift", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted drift intent"},
	{Intent: "hcmnext.operations.simulate_repair", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted repair simulation intent"},
	{Intent: "hcmnext.people.change_manager", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines", Reason: "effective-dating consumer wiring is a follow-up to the drafted people intent"},
	{Intent: "hcmnext.people.change_manager", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines", Reason: "eligibility consumer wiring is a follow-up to the drafted people intent"},
	{Intent: "hcmnext.people.change_manager", Computation: "schedule", Package: "internal/engines/schedule", Owner: "shared-engines", Reason: "schedule consumer wiring is a follow-up to trigger integration"},
	{Intent: "hcmnext.people.change_manager", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted people intent"},
	{Intent: "hcmnext.people.explain_worker_state", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines", Reason: "effective-dating consumer wiring is a follow-up to the drafted analytical intent"},
	{Intent: "hcmnext.people.promote_worker", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines", Reason: "effective-dating consumer wiring is a follow-up to the drafted promotion intent"},
	{Intent: "hcmnext.people.promote_worker", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines", Reason: "eligibility consumer wiring is a follow-up to the drafted promotion intent"},
	{Intent: "hcmnext.people.promote_worker", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines", Reason: "pay-band consumer wiring is a follow-up to the drafted promotion intent"},
	{Intent: "hcmnext.people.promote_worker", Computation: "population", Package: "internal/engines/population", Owner: "shared-engines", Reason: "population consumer wiring is a follow-up to population-scope closure"},
	{Intent: "hcmnext.people.promote_worker", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted promotion intent"},
	{Intent: "hcmnext.rewards.change_base_pay", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines", Reason: "effective-dating consumer wiring is a follow-up to the drafted compensation intent"},
	{Intent: "hcmnext.rewards.change_base_pay", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines", Reason: "pay-band consumer wiring is a follow-up to the drafted compensation intent"},
	{Intent: "hcmnext.rewards.evaluate_pay_band_position", Computation: "effective dating", Package: "internal/engines/effectivedate", Owner: "shared-engines", Reason: "effective-dating consumer wiring is a follow-up to the drafted pay-band intent"},
	{Intent: "hcmnext.rewards.evaluate_pay_band_position", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines", Reason: "pay-band consumer wiring is a follow-up to the drafted pay-band intent"},
	{Intent: "hcmnext.rewards.release_compensation_budget", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines", Reason: "eligibility consumer wiring is a follow-up to the drafted budget intent"},
	{Intent: "hcmnext.rewards.reserve_compensation_budget", Computation: "eligibility", Package: "internal/engines/eligibility", Owner: "shared-engines", Reason: "eligibility consumer wiring is a follow-up to the drafted budget intent"},
	{Intent: "hcmnext.rewards.reserve_compensation_budget", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted budget intent"},
	{Intent: "hcmnext.rewards.simulate_compensation", Computation: "pay band", Package: "internal/engines/payband", Owner: "shared-engines", Reason: "pay-band consumer wiring is a follow-up to the drafted compensation simulation intent"},
	{Intent: "hcmnext.work.approve_proposal", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted approval intent"},
	{Intent: "hcmnext.work.reject_proposal", Computation: "snapshot", Package: "internal/engines/snapshot", Owner: "shared-engines", Reason: "snapshot consumer wiring is a follow-up to the drafted rejection intent"},
}

func (o Options) effective() Options {
	if o.Responsibilities == nil {
		o.Responsibilities = DefaultResponsibilities()
	}
	if o.Allowlist == nil {
		o.Allowlist = append([]ImplicitEntry(nil), ImplicitAllowlist...)
	}
	if o.ImportRoots == nil {
		o.ImportRoots = []string{"internal/intent/definitions", "internal/intent/app", "tools/conformance"}
	}
	return o
}

// Scan validates the table and scans the allowed intent-definition, app, and
// conformance trees with go/parser for imports of each named engine.
func Scan(root string, options Options) (Report, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("enginecoverage: resolve root: %w", err)
	}
	options = options.effective()
	report := Report{Responsibilities: append([]Responsibility(nil), options.Responsibilities...)}
	allow := make(map[string]ImplicitEntry, len(options.Allowlist))
	for _, entry := range options.Allowlist {
		if entry.Intent != "" && entry.Computation != "" && entry.Package != "" && entry.Owner != "" && entry.Reason != "" {
			allow[findingKey(entry.Intent, entry.Computation, entry.Package)] = entry
		}
	}
	seenComputations := map[string]Responsibility{}
	seenPackages := map[string]Responsibility{}
	checkedPackages := map[string]bool{}
	imports, err := scanImports(absRoot, options.ImportRoots)
	if err != nil {
		return Report{}, err
	}
	for _, row := range options.Responsibilities {
		if row.Intent == "" || row.Computation == "" || row.Package == "" || row.Owner == "" {
			report.Findings = append(report.Findings, Finding{Kind: "INVALID", Intent: row.Intent, Computation: row.Computation, Package: row.Package, Detail: "responsibility requires intent, computation, package, and owner"})
			continue
		}
		computationKey := row.Intent + "\x00" + row.Computation
		if old, exists := seenComputations[computationKey]; exists && old.Package != row.Package {
			report.Findings = append(report.Findings, Finding{Kind: "DUPLICATE_RESPONSIBILITY", Intent: row.Intent, Computation: row.Computation, Package: row.Package, Detail: "computation has more than one engine owner"})
		} else {
			seenComputations[computationKey] = row
		}
		if old, exists := seenPackages[row.Package]; exists && old.Computation != row.Computation {
			// A package may own several computations; this is not a gap. The
			// map is used only to make package validation happen once below.
			_ = old
		} else {
			seenPackages[row.Package] = row
		}
		if !checkedPackages[row.Package] {
			checkPackage(absRoot, row.Package, &report)
			checkedPackages[row.Package] = true
		}
		key := findingKey(row.Intent, row.Computation, row.Package)
		if !imports[row.Package] {
			finding := Finding{Kind: "IMPLICIT", Intent: row.Intent, Computation: row.Computation, Package: row.Package, Detail: "named engine is not imported by an intent definition, app, or conformance package"}
			if exception, ok := allow[key]; ok {
				finding.Allowlisted = true
				finding.Owner = exception.Owner
				finding.Detail = exception.Reason
			}
			report.Findings = append(report.Findings, finding)
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		ak := a.Intent + "\x00" + a.Computation + "\x00" + a.Package + "\x00" + a.Kind
		bk := b.Intent + "\x00" + b.Computation + "\x00" + b.Package + "\x00" + b.Kind
		return ak < bk
	})
	return report, nil
}

// Check scans with the default table and returns the first unallowlisted gap.
func Check(root string) error {
	report, err := Scan(root, Options{})
	if err != nil {
		return err
	}
	if violations := report.Violations(); len(violations) != 0 {
		gap := violations[0]
		return fmt.Errorf("enginecoverage: %s %s/%s %s: %s", gap.Kind, gap.Intent, gap.Computation, gap.Package, gap.Detail)
	}
	return nil
}

func findingKey(intent, computation, pkg string) string {
	return intent + "\x00" + computation + "\x00" + pkg
}

func checkPackage(root, rel string, report *Report) {
	dir := filepath.Join(root, filepath.FromSlash(rel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		report.Findings = append(report.Findings, Finding{Kind: "MISSING_PACKAGE", Package: rel, Detail: "named engine package does not exist"})
		return
	}
	var hasVersion, hasExplain bool
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			report.Findings = append(report.Findings, Finding{Kind: "PARSE_ERROR", Package: rel, Detail: err.Error()})
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				if ok && fn.Name.Name == "Explain" {
					hasExplain = true
				}
				continue
			}
			if fn.Name.Name == "Version" {
				hasVersion = true
			}
			if fn.Name.Name == "Explain" {
				hasExplain = true
			}
		}
	}
	if !hasVersion {
		report.Findings = append(report.Findings, Finding{Kind: "MISSING_VERSION", Package: rel, Detail: "engine does not export Version"})
	}
	if !hasExplain {
		report.Findings = append(report.Findings, Finding{Kind: "MISSING_EXPLAIN", Package: rel, Detail: "engine does not export an Explain-shaped symbol"})
	}
}

func scanImports(root string, relRoots []string) (map[string]bool, error) {
	imports := map[string]bool{}
	for _, relRoot := range relRoots {
		dir := filepath.Join(root, filepath.FromSlash(relRoot))
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("enginecoverage: stat import root %s: %w", relRoot, err)
		}
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			for _, spec := range file.Imports {
				pathValue, err := strconvUnquote(spec.Path.Value)
				if err != nil {
					return err
				}
				prefix := modulePath + "/"
				if strings.HasPrefix(pathValue, prefix) && strings.HasPrefix(pathValue[len(prefix):], "internal/engines/") {
					engine := pathValue[len(prefix):]
					parts := strings.Split(engine, "/")
					if len(parts) >= 3 {
						imports["internal/engines/"+parts[2]] = true
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("enginecoverage: scan imports under %s: %w", relRoot, err)
		}
	}
	return imports, nil
}

func strconvUnquote(value string) (string, error) {
	if len(value) < 2 || value[0] != '"' {
		return "", fmt.Errorf("enginecoverage: invalid import literal %q", value)
	}
	decoded, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("enginecoverage: unquote import %q: %w", value, err)
	}
	return decoded, nil
}
