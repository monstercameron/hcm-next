// Package archrules implements the ARCH-GO-004 through ARCH-GO-013
// architecture checks: kernel dependency-minimalism, intent/capability
// ownership, governance composition, workflow layer separation, engine
// independence and contract shape, domain ownership, transaction/conflict
// direction, ledger/PostgreSQL independence, and port ownership.
//
// Every check reuses the real `go list -json` import graph
// (tools/policy/internal/repopath) and the rule data in
// definitions/architecture/architecture-rules.yaml, which supplements
// (without editing) repository-layout.yaml and package-dependency-policy.yaml.
package archrules

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Kernel is the ARCH-GO-004 rule data.
type Kernel struct {
	Root                          string   `yaml:"root"`
	AllowedExternalImportPrefixes []string `yaml:"allowed_external_import_prefixes"`
}

// IntentCapability is the ARCH-GO-005 rule data.
type IntentCapability struct {
	IntentRoot                 string   `yaml:"intent_root"`
	CapabilityRoot             string   `yaml:"capability_root"`
	ForbiddenCapabilityImports []string `yaml:"forbidden_capability_imports"`
	CapabilityForbiddenContent []string `yaml:"capability_forbidden_content_markers"`
}

// GovernanceComposition is the ARCH-GO-006 rule data.
type GovernanceComposition struct {
	GovernanceRoot string   `yaml:"governance_root"`
	ComposedRoots  []string `yaml:"composed_roots"`
}

// Edge is a named forbidden importer/imported root pair.
type Edge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// WorkflowLayers is the ARCH-GO-007 rule data.
type WorkflowLayers struct {
	DefinitionRoot           string   `yaml:"definition_root"`
	CompilerRoot             string   `yaml:"compiler_root"`
	RuntimeRoot              string   `yaml:"runtime_root"`
	StepRoot                 string   `yaml:"step_root"`
	ForbiddenEdges           []Edge   `yaml:"forbidden_edges"`
	CompilerForbiddenContent []string `yaml:"compiler_forbidden_content_markers"`
	DomainPersistenceMarkers []string `yaml:"domain_persistence_markers"`
}

// Engines is the ARCH-GO-008 rule data.
type Engines struct {
	Root string `yaml:"root"`
}

// EngineContract is the ARCH-GO-009 rule data.
type EngineContract struct {
	Root            string     `yaml:"root"`
	RequiredSymbols []string   `yaml:"required_symbols"`
	PairedSymbols   [][]string `yaml:"paired_symbols"`
	RequiredAnyOf   [][]string `yaml:"required_any_of"`
}

// DomainOwnership is the ARCH-GO-010 rule data.
type DomainOwnership struct {
	Root                        string   `yaml:"root"`
	ForbiddenCentralRepoPackage string   `yaml:"forbidden_central_repository_package"`
	PersistenceMarkers          []string `yaml:"persistence_markers"`
}

// TransactionConflict is the ARCH-GO-011 rule data.
type TransactionConflict struct {
	TransactionRoot string `yaml:"transaction_root"`
	ConflictRoot    string `yaml:"conflict_root"`
	ForbiddenEdges  []Edge `yaml:"forbidden_edges"`
}

// LedgerIndependent is the ARCH-GO-012 rule data.
type LedgerIndependent struct {
	LedgerRoot              string   `yaml:"ledger_root"`
	ForbiddenImportPrefixes []string `yaml:"forbidden_import_prefixes"`
}

// PortOwnership is the ARCH-GO-013 rule data.
type PortOwnership struct {
	ForbiddenCentralRepoPackage string   `yaml:"forbidden_central_repository_package"`
	AdapterMarkers              []string `yaml:"adapter_markers"`
}

// Config is the parsed form of architecture-rules.yaml.
type Config struct {
	Version int    `yaml:"version"`
	Module  string `yaml:"module"`

	Kernel                Kernel                `yaml:"kernel"`
	IntentCapability      IntentCapability      `yaml:"intent_capability"`
	GovernanceComposition GovernanceComposition `yaml:"governance_composition"`
	WorkflowLayers        WorkflowLayers        `yaml:"workflow_layers"`
	Engines               Engines               `yaml:"engines"`
	EngineContract        EngineContract        `yaml:"engine_contract"`
	DomainOwnership       DomainOwnership       `yaml:"domain_ownership"`
	TransactionConflict   TransactionConflict   `yaml:"transaction_conflict"`
	LedgerIndependent     LedgerIndependent     `yaml:"ledger_independent"`
	PortOwnership         PortOwnership         `yaml:"port_ownership"`
	Ceremony              CeremonyRules         `yaml:"ceremony"`
}

// Load reads and parses the manifest at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("archrules: reading config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("archrules: parsing config: %w", err)
	}
	if c.Module == "" {
		return nil, fmt.Errorf("archrules: config has no module")
	}
	return &c, nil
}

// TrimModule strips the Human Capital Management Suite module prefix from a full import path,
// returning (relative path, true), or ("", false) when importPath is not
// part of module.
func TrimModule(module, importPath string) (string, bool) {
	if importPath == module {
		return "", true
	}
	prefix := module + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(importPath, prefix), true
}

// UnderRoot reports whether rel is root itself or a subpackage of root.
func UnderRoot(rel, root string) bool {
	return rel == root || strings.HasPrefix(rel, root+"/")
}

// UnderAnyRoot reports whether rel is under any of roots.
func UnderAnyRoot(rel string, roots []string) bool {
	for _, r := range roots {
		if UnderRoot(rel, r) {
			return true
		}
	}
	return false
}

// MatchGlob matches a single-"*"-wildcard-segment glob (as used throughout
// definitions/architecture/*.yaml, e.g. "internal/domains/*/store") against
// a module-relative path.
func MatchGlob(glob, rel string) bool {
	globSegs := strings.Split(glob, "/")
	relSegs := strings.Split(rel, "/")
	if len(relSegs) < len(globSegs) {
		return false
	}
	for i, g := range globSegs {
		if g == "*" {
			continue
		}
		if g != relSegs[i] {
			return false
		}
	}
	return true
}

// MatchesAnyGlob reports whether rel matches any glob in globs.
func MatchesAnyGlob(globs []string, rel string) bool {
	for _, g := range globs {
		if MatchGlob(g, rel) {
			return true
		}
	}
	return false
}

// Violation names one forbidden edge or content marker, with the rule that
// was broken.
type Violation struct {
	Rule     string
	Importer string
	Imported string
	Detail   string
}

// CheckForbiddenEdges evaluates importerRel -> importedRel against a list
// of named forbidden root-pair edges (as ARCH-GO-007's workflow_layers and
// ARCH-GO-011's transaction_conflict encode them), returning a Violation
// when importerRel is under edge.From and importedRel is under edge.To.
func CheckForbiddenEdges(rule string, edges []Edge, importerRel, importedRel string) *Violation {
	for _, e := range edges {
		if UnderRoot(importerRel, e.From) && UnderRoot(importedRel, e.To) {
			return &Violation{Rule: rule, Importer: importerRel, Imported: importedRel}
		}
	}
	return nil
}

// CheckExternalImportAllowlist evaluates one kernel-owned package's
// non-standard-library import against Kernel.AllowedExternalImportPrefixes
// (ARCH-GO-004). importedPath is a full Go import path; stdlib packages
// (no dot in the first path segment) are always allowed and never passed
// to this function by callers that pre-filter, but it is also safe to call
// on one directly since a stdlib path never matches "module/root" imports
// either.
func CheckExternalImportAllowlist(k Kernel, hcmModule, importerRel, importedPath string) *Violation {
	if !UnderRoot(importerRel, k.Root) {
		return nil
	}
	if isStdlib(importedPath) {
		return nil
	}
	if strings.HasPrefix(importedPath, hcmModule+"/") || importedPath == hcmModule {
		// An intra-module import is ARCH-GO-002/depedge's concern (kernel
		// must not import upward), not this external-dependency allowlist.
		return nil
	}
	for _, prefix := range k.AllowedExternalImportPrefixes {
		if importedPath == prefix || strings.HasPrefix(importedPath, prefix+"/") {
			return nil
		}
	}
	return &Violation{Rule: "kernel-external-import-not-allowlisted", Importer: importerRel, Imported: importedPath}
}

// engineName returns the first path segment of rel below enginesRoot (the
// engine's own directory name), or "" if rel is not under enginesRoot at
// all.
func engineName(enginesRoot, rel string) string {
	if !UnderRoot(rel, enginesRoot) {
		return ""
	}
	suffix := strings.TrimPrefix(rel, enginesRoot+"/")
	if suffix == rel {
		return "" // rel == enginesRoot itself; not one specific engine
	}
	if i := strings.Index(suffix, "/"); i >= 0 {
		return suffix[:i]
	}
	return suffix
}

// CheckEngineCrossImport is the ARCH-GO-008 cross-engine rule: one engine
// may import a sibling engine's own root package (its public, versioned
// contract) but never a subpackage beneath it (an unexported-by-convention
// implementation detail). Importing within one's own engine, or importing
// something outside enginesRoot entirely, is never a violation here.
func CheckEngineCrossImport(enginesRoot, importerRel, importedRel string) *Violation {
	importerEngine := engineName(enginesRoot, importerRel)
	importedEngine := engineName(enginesRoot, importedRel)
	if importerEngine == "" || importedEngine == "" || importerEngine == importedEngine {
		return nil
	}
	importedEngineRoot := enginesRoot + "/" + importedEngine
	if importedRel == importedEngineRoot {
		return nil // importing the sibling engine's own root contract package is fine
	}
	return &Violation{Rule: "engine-must-not-import-sibling-engine-subpackage", Importer: importerRel, Imported: importedRel}
}

// CheckEngineContract evaluates one engine package's exported top-level
// name set (as ExportedTopLevelNames/ExportedTopLevelNamesFromSource
// produce it) against EngineContract's required shape (ARCH-GO-009),
// returning a human-readable missing-requirement string per gap, or nil
// when the package satisfies the contract. A method name recorded as
// "Type.Method" also satisfies a bare required symbol name match against
// "Method" (a required contract symbol may legitimately be a method on the
// engine's own result/table type rather than a free function).
func CheckEngineContract(c EngineContract, exportedNames map[string]bool) []string {
	has := func(symbol string) bool {
		if exportedNames[symbol] {
			return true
		}
		for name := range exportedNames {
			if strings.HasSuffix(name, "."+symbol) {
				return true
			}
		}
		return false
	}

	var missing []string
	for _, sym := range c.RequiredSymbols {
		if !has(sym) {
			missing = append(missing, "missing required symbol "+sym)
		}
	}
	for _, pair := range c.PairedSymbols {
		if len(pair) != 2 {
			continue
		}
		a, b := has(pair[0]), has(pair[1])
		if a != b {
			missing = append(missing, fmt.Sprintf("has %s without its pair %s (or vice versa)", pair[0], pair[1]))
		}
	}
	for _, alts := range c.RequiredAnyOf {
		found := false
		for _, alt := range alts {
			if has(alt) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("missing at least one of %v", alts))
		}
	}
	return missing
}

// CheckConflictImportsCoordinator is the ARCH-GO-011 direction check.
// tc.ConflictRoot is a subpackage of tc.TransactionRoot, so a naive
// "importer under conflict, imported under transaction" test would also
// flag conflict's own intra-package imports (conflict is, after all, under
// transaction too); this checks the imported path against the coordinator
// specifically, excluding anything still inside the conflict subtree
// itself.
func CheckConflictImportsCoordinator(tc TransactionConflict, importerRel, importedRel string) *Violation {
	if !UnderRoot(importerRel, tc.ConflictRoot) {
		return nil
	}
	if UnderRoot(importedRel, tc.ConflictRoot) {
		return nil // intra-conflict import, always fine
	}
	if UnderRoot(importedRel, tc.TransactionRoot) {
		return &Violation{Rule: "conflict-must-not-import-coordinator", Importer: importerRel, Imported: importedRel}
	}
	return nil
}

// isStdlib is a conservative heuristic: a standard-library import path's
// first segment never contains a "." (every third-party module path does,
// being a domain name), and it never starts with the Human Capital Management Suite module's own
// host segment. It intentionally does not attempt to be a complete stdlib
// registry; ARCH-GO-004's allowlist is additive over "always allow
// standard library", so a false negative here (treating some obscure path
// as non-stdlib) only produces an overly strict, investigable finding, never
// a silently-passed real violation.
func isStdlib(importPath string) bool {
	first := importPath
	if i := strings.Index(importPath, "/"); i >= 0 {
		first = importPath[:i]
	}
	return !strings.Contains(first, ".")
}
