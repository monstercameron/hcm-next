// Package decomposition validates evidence required before introducing a
// module, process, or network-service boundary (ARCH-GO-019).
package decomposition

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Claim binds a measurement to the artifact that produced it. Keeping Value
// and Limit separate prevents prose containing "measured" from passing.
type Claim struct {
	Source  string  `json:"source"`
	Measure string  `json:"measure"`
	Value   float64 `json:"value"`
	Limit   float64 `json:"limit"`
	Unit    string  `json:"unit"`
	Outcome string  `json:"outcome"`
}

type Contract struct {
	Source      string `json:"source"`
	Authority   string `json:"authority"`
	Version     string `json:"version"`
	Consistency string `json:"consistency"`
}

type Authority struct {
	Source string `json:"source"`
	System string `json:"system"`
	Writer string `json:"writer"`
}

type Operation struct {
	Source    string `json:"source"`
	Failure   string `json:"failure"`
	Detection string `json:"detection"`
	Repair    string `json:"repair"`
	Rollback  string `json:"rollback"`
}

type Cost struct {
	Source        string `json:"source"`
	MonthlyMinor  int64  `json:"monthly_minor"`
	Currency      string `json:"currency"`
	OnCallOwner   string `json:"on_call_owner"`
	AddedAlerts   int    `json:"added_alerts"`
	AddedRunbooks int    `json:"added_runbooks"`
}

type Alternative struct {
	Source  string `json:"source"`
	Option  string `json:"option"`
	Outcome string `json:"outcome"`
}

type Evidence struct {
	Scaling            Claim       `json:"scaling"`
	FailureIsolation   Claim       `json:"failure_isolation"`
	SecurityBoundary   Claim       `json:"security_boundary"`
	Residency          Claim       `json:"residency"`
	ReleaseBoundary    Claim       `json:"release_boundary"`
	OwnershipBoundary  Claim       `json:"ownership_boundary"`
	APIEventContract   Contract    `json:"api_event_contract"`
	DataAuthority      Authority   `json:"data_authority"`
	FailureRepair      Operation   `json:"failure_repair"`
	MigrationRollback  Operation   `json:"migration_rollback"`
	OperationalCost    Cost        `json:"operational_cost"`
	MonolithExperiment Claim       `json:"monolith_experiment"`
	ExistingRoleOption Alternative `json:"existing_role_option"`
	ScaleProcessOption Alternative `json:"scale_process_option"`
}

type Decision struct {
	Version         int                 `json:"version"`
	ID              string              `json:"id"`
	Kind            string              `json:"kind"`
	Target          string              `json:"target"`
	Justification   string              `json:"justification"`
	Evidence        Evidence            `json:"evidence"`
	DomainPackages  []string            `json:"domain_packages"`
	Processes       map[string][]string `json:"processes"`
	EvidenceDigests map[string]string   `json:"evidence_digests"`
}

// BoundaryIndex is the reviewed inventory for physical boundaries. Baseline
// identities are exact (never wildcarded); reviewed paths authorize only
// newly observed boundaries.
type BoundaryIndex struct {
	Version  int        `json:"version"`
	Baseline []Boundary `json:"baseline"`
	Reviewed []Review   `json:"reviewed"`
}

type Boundary struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
}

type Review struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
	Path     string `json:"path"`
}

// initialBaseline is the immutable pre-gate inventory. An index may omit a
// seed (the live-tree comparison will then fail if it still exists), but it
// cannot turn a new boundary into legacy state by appending to baseline.
var initialBaseline = map[Boundary]bool{
	{Kind: "module", Identity: "go.mod"}:                      true,
	{Kind: "module", Identity: "src/blocks/go/go.mod"}:        true,
	{Kind: "process", Identity: "cmd/frontenddev"}:            true,
	{Kind: "process", Identity: "cmd/hcmctl"}:                 true,
	{Kind: "process", Identity: "cmd/hcmnext"}:                true,
	{Kind: "process", Identity: "cmd/migrate"}:                true,
	{Kind: "process", Identity: "cmd/projector"}:              true,
	{Kind: "process", Identity: "cmd/scheduler"}:              true,
	{Kind: "process", Identity: "cmd/worker"}:                 true,
	{Kind: "process", Identity: "src/blocks/go/cmd/executor"}: true,
}

const (
	MissingEvidence      = "MISSING_EVIDENCE"
	UnmeasuredEvidence   = "UNMEASURED_EVIDENCE"
	TopologyOnly         = "TOPOLOGY_ONLY"
	DuplicateDomainOwner = "DUPLICATE_DOMAIN_OWNER"
	MissingKind          = "MISSING_KIND"
	MissingTarget        = "MISSING_TARGET"
	InvalidKind          = "INVALID_KIND"
	MissingProcess       = "MISSING_PROCESS"
	MissingInventory     = "MISSING_INVENTORY"
	InventoryMismatch    = "INVENTORY_MISMATCH"
)

type Finding struct{ DecisionID, Code, Field, Detail string }

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s: %s (%s)", f.DecisionID, f.Field, f.Detail, f.Code)
}

func Validate(d Decision) []Finding {
	var out []Finding
	add := func(code, field, detail string) { out = append(out, Finding{d.ID, code, field, detail}) }
	if d.Version != 2 {
		add(MissingEvidence, "version", "decision format version must be 2")
	}
	if strings.TrimSpace(d.Kind) == "" {
		add(MissingKind, "kind", "decomposition kind is required")
	} else if !allowedKind(d.Kind) {
		add(InvalidKind, "kind", "kind must be module, process, or service")
	}
	if strings.TrimSpace(d.Target) == "" {
		add(MissingTarget, "target", "decomposition target is required")
	}
	if strings.TrimSpace(d.Justification) == "" || topologyOnly(d.Justification+" "+d.Target) {
		add(TopologyOnly, "justification", "team naming, domain count, or deployment fashion is not a measured constraint")
	}
	if strings.EqualFold(strings.TrimSpace(d.Kind), "process") {
		if len(d.DomainPackages) == 0 {
			add(MissingInventory, "domain_packages", "process decision must name its semantic packages")
		}
		if len(d.Processes) == 0 {
			add(MissingProcess, "processes", "process decision must include semantic ownership")
		}
	}
	claims := []struct {
		name     string
		claim    Claim
		exceeded bool
	}{
		{"scaling", d.Evidence.Scaling, true}, {"failure_isolation", d.Evidence.FailureIsolation, false},
		{"security_boundary", d.Evidence.SecurityBoundary, false}, {"residency", d.Evidence.Residency, false},
		{"release_boundary", d.Evidence.ReleaseBoundary, false}, {"ownership_boundary", d.Evidence.OwnershipBoundary, false},
		{"monolith_experiment", d.Evidence.MonolithExperiment, true},
	}
	for _, item := range claims {
		validateClaim(add, "evidence."+item.name, item.claim, item.exceeded)
	}
	requireSource(add, "evidence.api_event_contract.source", d.Evidence.APIEventContract.Source)
	require(add, "evidence.api_event_contract.authority", d.Evidence.APIEventContract.Authority)
	require(add, "evidence.api_event_contract.version", d.Evidence.APIEventContract.Version)
	require(add, "evidence.api_event_contract.consistency", d.Evidence.APIEventContract.Consistency)
	requireSource(add, "evidence.data_authority.source", d.Evidence.DataAuthority.Source)
	require(add, "evidence.data_authority.system", d.Evidence.DataAuthority.System)
	require(add, "evidence.data_authority.writer", d.Evidence.DataAuthority.Writer)
	validateOperation(add, "evidence.failure_repair", d.Evidence.FailureRepair)
	validateOperation(add, "evidence.migration_rollback", d.Evidence.MigrationRollback)
	requireSource(add, "evidence.operational_cost.source", d.Evidence.OperationalCost.Source)
	require(add, "evidence.operational_cost.on_call_owner", d.Evidence.OperationalCost.OnCallOwner)
	require(add, "evidence.operational_cost.currency", d.Evidence.OperationalCost.Currency)
	if d.Evidence.OperationalCost.MonthlyMinor <= 0 || d.Evidence.OperationalCost.AddedAlerts <= 0 || d.Evidence.OperationalCost.AddedRunbooks <= 0 {
		add(UnmeasuredEvidence, "evidence.operational_cost", "cost, alerts, and runbooks must quantify the added operating burden")
	}
	validateAlternative(add, "evidence.existing_role_option", d.Evidence.ExistingRoleOption)
	validateAlternative(add, "evidence.scale_process_option", d.Evidence.ScaleProcessOption)

	owners := map[string]string{}
	roles := make([]string, 0, len(d.Processes))
	for role := range d.Processes {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		if strings.TrimSpace(role) == "" {
			add(MissingProcess, "processes", "process role is required")
			continue
		}
		for _, raw := range d.Processes[role] {
			domain := strings.TrimSpace(raw)
			if domain == "" {
				continue
			}
			if prior, ok := owners[domain]; ok && prior != role {
				add(DuplicateDomainOwner, "processes."+role, fmt.Sprintf("domain package %q is also owned by process %q", domain, prior))
			} else {
				owners[domain] = role
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

func validateClaim(add func(string, string, string), field string, c Claim, exceeded bool) {
	requireSource(add, field+".source", c.Source)
	require(add, field+".measure", c.Measure)
	require(add, field+".unit", c.Unit)
	require(add, field+".outcome", c.Outcome)
	if c.Limit <= 0 || c.Value <= 0 {
		add(UnmeasuredEvidence, field, "value and limit must be positive measurements")
	} else if exceeded && c.Value <= c.Limit {
		add(UnmeasuredEvidence, field, "measurement does not exceed the recorded monolith limit")
	}
}

func validateOperation(add func(string, string, string), field string, o Operation) {
	requireSource(add, field+".source", o.Source)
	require(add, field+".failure", o.Failure)
	require(add, field+".detection", o.Detection)
	require(add, field+".repair", o.Repair)
	require(add, field+".rollback", o.Rollback)
}

func validateAlternative(add func(string, string, string), field string, a Alternative) {
	requireSource(add, field+".source", a.Source)
	require(add, field+".option", a.Option)
	require(add, field+".outcome", a.Outcome)
}

func require(add func(string, string, string), field, value string) {
	if strings.TrimSpace(value) == "" {
		add(MissingEvidence, field, "required sourced evidence is missing")
	}
}

func requireSource(add func(string, string, string), field, value string) {
	require(add, field, value)
	value = strings.TrimSpace(value)
	if value != "" && !strings.Contains(value, "/") && !strings.Contains(value, `\`) {
		add(MissingEvidence, field, "evidence source must be a repository path or URL")
	}
}

func Check(d Decision) error {
	if findings := Validate(d); len(findings) > 0 {
		return errors.New(findings[0].String())
	}
	return nil
}

// CheckInventory prevents a decision from proving unique ownership with an
// invented subset of the repository's process-role manifest. Inventory maps
// an actual process command to its declared semantic packages.
func CheckInventory(d Decision, inventory map[string][]string) error {
	var findings []Finding
	actual := map[string][]string{}
	for process, packages := range inventory {
		for _, pkg := range packages {
			actual[strings.TrimSpace(pkg)] = append(actual[strings.TrimSpace(pkg)], process)
		}
	}
	declared := make(map[string]bool, len(d.DomainPackages))
	for _, pkg := range d.DomainPackages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if declared[pkg] {
			findings = append(findings, Finding{d.ID, InventoryMismatch, "domain_packages", fmt.Sprintf("package %q is declared more than once", pkg)})
		}
		declared[pkg] = true
		var submitted []string
		for process, packages := range d.Processes {
			for _, candidate := range packages {
				if strings.TrimSpace(candidate) == pkg {
					submitted = append(submitted, process)
				}
			}
		}
		sort.Strings(submitted)
		owners := actual[pkg]
		sort.Strings(owners)
		if len(owners) == 0 {
			findings = append(findings, Finding{d.ID, MissingInventory, "domain_packages", fmt.Sprintf("package %q is absent from the repository process-role inventory", pkg)})
		} else if strings.Join(owners, "\x00") != strings.Join(submitted, "\x00") {
			findings = append(findings, Finding{d.ID, InventoryMismatch, "processes", fmt.Sprintf("package %q owners are %v in the repository inventory, not %v", pkg, owners, submitted)})
		}
	}
	for process, packages := range d.Processes {
		for _, raw := range packages {
			pkg := strings.TrimSpace(raw)
			if pkg != "" && !declared[pkg] {
				findings = append(findings, Finding{d.ID, InventoryMismatch, "processes." + process, fmt.Sprintf("package %q is omitted from domain_packages", pkg)})
			}
		}
	}
	if len(findings) > 0 {
		return errors.New(findings[0].String())
	}
	return nil
}

// ReadFile strictly decodes one decision record.
func ReadFile(path string) (Decision, error) {
	var d Decision
	if strings.TrimSpace(path) == "" {
		return d, errors.New("decomposition: decision path is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return d, fmt.Errorf("decomposition: read decision: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, fmt.Errorf("decomposition: parse decision: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return d, errors.New("decomposition: parse decision: multiple JSON values")
		}
		return d, fmt.Errorf("decomposition: parse decision: %w", err)
	}
	return d, nil
}

func CheckFile(path string) error {
	d, err := ReadFile(path)
	if err != nil {
		return err
	}
	return Check(d)
}

// ScanRoot discovers physical process/module/service boundary identities and
// requires the exact reviewed index to account for every change.
func ScanRoot(root, indexPath string) error {
	return ScanRootWithInventory(root, indexPath, nil)
}

// ScanRootWithInventory additionally binds process decisions to the live
// semantic-package ownership manifest. Production callers must supply it.
func ScanRootWithInventory(root, indexPath string, inventory map[string][]string) error {
	idx, err := readIndex(indexPath)
	if err != nil {
		return err
	}
	observed, err := discoverBoundaries(root)
	if err != nil {
		return err
	}
	baseline := make(map[string]Boundary, len(idx.Baseline))
	for _, b := range idx.Baseline {
		key := b.Kind + "\x00" + b.Identity
		if !initialBaseline[b] {
			return fmt.Errorf("decomposition: boundary is not in frozen initial baseline: %s:%s", b.Kind, b.Identity)
		}
		if baseline[key].Identity != "" {
			return errors.New("decomposition: invalid or duplicate baseline boundary")
		}
		baseline[key] = b
	}
	reviewed := make(map[string]Review, len(idx.Reviewed))
	// Reject identity collisions before touching referenced files. This keeps
	// malformed indexes fail-closed even when a colliding review path is absent.
	for _, r := range idx.Reviewed {
		key := r.Kind + "\x00" + r.Identity
		if !allowedKind(r.Kind) || r.Identity == "" || r.Path == "" || reviewed[key].Identity != "" || baseline[key].Identity != "" {
			return errors.New("decomposition: invalid or duplicate reviewed boundary")
		}
		reviewed[key] = r
	}
	reviewed = make(map[string]Review, len(idx.Reviewed))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("decomposition: resolve root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("decomposition: resolve root symlinks: %w", err)
	}
	for _, r := range idx.Reviewed {
		key := r.Kind + "\x00" + r.Identity
		if !allowedKind(r.Kind) || r.Identity == "" || r.Path == "" || reviewed[key].Identity != "" || baseline[key].Identity != "" {
			return errors.New("decomposition: invalid or duplicate reviewed boundary")
		}
		clean := filepath.Clean(r.Path)
		if filepath.IsAbs(r.Path) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			return fmt.Errorf("decomposition: reviewed decision path escapes root: %s", r.Path)
		}
		joined := filepath.Join(rootReal, clean)
		real, err := filepath.EvalSymlinks(joined)
		if err != nil {
			return fmt.Errorf("decomposition: reviewed decision path %q: %w", r.Path, err)
		}
		rel, err := filepath.Rel(rootReal, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("decomposition: reviewed decision path escapes root: %s", r.Path)
		}
		r.Path = clean
		reviewed[key] = r
	}
	seen := make(map[string]bool, len(observed))
	for _, b := range observed {
		key := b.Kind + "\x00" + b.Identity
		seen[key] = true
		if baseline[key].Identity != "" {
			continue
		}
		review, ok := reviewed[key]
		if !ok {
			return fmt.Errorf("decomposition: unreviewed new boundary %s:%s", b.Kind, b.Identity)
		}
		d, err := ReadFile(filepath.Join(rootReal, review.Path))
		if err != nil {
			return err
		}
		if err := Check(d); err != nil {
			return fmt.Errorf("decomposition: reviewed %s: %w", review.Path, err)
		}
		if d.Kind != b.Kind || d.Target != b.Identity {
			return fmt.Errorf("decomposition: reviewed decision kind/target %s:%s does not match boundary %s:%s", d.Kind, d.Target, b.Kind, b.Identity)
		}
		if err := VerifyEvidence(rootReal, d); err != nil {
			return fmt.Errorf("decomposition: reviewed %s evidence: %w", review.Path, err)
		}
		if b.Kind == "process" {
			if inventory == nil {
				return errors.New("decomposition: process inventory is required for reviewed process boundary")
			}
			targetRole := filepath.Base(filepath.Clean(d.Target))
			if len(d.Processes[targetRole]) == 0 {
				return fmt.Errorf("decomposition: reviewed %s ownership: new process %q owns no declared semantic package", review.Path, targetRole)
			}
			if err := CheckInventory(d, inventory); err != nil {
				return fmt.Errorf("decomposition: reviewed %s ownership: %w", review.Path, err)
			}
		}
	}
	for key, review := range reviewed {
		if !seen[key] {
			return fmt.Errorf("decomposition: reviewed identity is not an observed boundary: %s:%s", review.Kind, review.Identity)
		}
	}
	for key, b := range baseline {
		if !seen[key] {
			return fmt.Errorf("decomposition: baseline boundary missing %s:%s", b.Kind, b.Identity)
		}
	}
	return nil
}

// VerifyEvidence binds every asserted fact to immutable, repository-contained
// bytes. URLs are not accepted directly because CI cannot authenticate their
// current contents; archive a local copy and pin its digest instead.
func VerifyEvidence(root string, d Decision) error {
	if d.Version != 2 {
		return errors.New("reviewed decision format version must be 2")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve evidence root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("resolve evidence root: %w", err)
	}
	for _, source := range evidenceSources(d) {
		digest := strings.ToLower(strings.TrimSpace(d.EvidenceDigests[source]))
		if len(digest) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(digest, "sha256:") {
			return fmt.Errorf("source %q requires a sha256 digest", source)
		}
		clean := filepath.Clean(source)
		if filepath.IsAbs(source) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("source escapes repository: %s", source)
		}
		path := filepath.Join(rootReal, clean)
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("source %q: %w", source, err)
		}
		rel, err := filepath.Rel(rootReal, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("source escapes repository: %s", source)
		}
		info, err := os.Stat(real)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("source %q must be a regular file", source)
		}
		data, err := os.ReadFile(real)
		if err != nil {
			return fmt.Errorf("read source %q: %w", source, err)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return fmt.Errorf("source %q is empty", source)
		}
		sum := sha256.Sum256(data)
		actual := fmt.Sprintf("sha256:%x", sum[:])
		if !strings.EqualFold(actual, strings.TrimSpace(d.EvidenceDigests[source])) {
			return fmt.Errorf("source %q digest mismatch", source)
		}
	}
	return nil
}

func evidenceSources(d Decision) []string {
	sources := []string{d.Evidence.Scaling.Source, d.Evidence.FailureIsolation.Source, d.Evidence.SecurityBoundary.Source, d.Evidence.Residency.Source, d.Evidence.ReleaseBoundary.Source, d.Evidence.OwnershipBoundary.Source, d.Evidence.APIEventContract.Source, d.Evidence.DataAuthority.Source, d.Evidence.FailureRepair.Source, d.Evidence.MigrationRollback.Source, d.Evidence.OperationalCost.Source, d.Evidence.MonolithExperiment.Source, d.Evidence.ExistingRoleOption.Source, d.Evidence.ScaleProcessOption.Source}
	seen := map[string]bool{}
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source != "" && !seen[source] {
			seen[source] = true
			out = append(out, source)
		}
	}
	sort.Strings(out)
	return out
}

func readIndex(path string) (BoundaryIndex, error) {
	var idx BoundaryIndex
	b, err := os.ReadFile(path)
	if err != nil {
		return idx, fmt.Errorf("decomposition: read boundary index: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&idx); err != nil {
		return idx, fmt.Errorf("decomposition: parse boundary index: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return idx, errors.New("decomposition: parse boundary index: multiple JSON values")
	}
	if idx.Version != 1 {
		return idx, errors.New("decomposition: boundary index version must be 1")
	}
	return idx, nil
}

func discoverBoundaries(root string) ([]Boundary, error) {
	var out []Boundary
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return nil, fmt.Errorf("decomposition: root go.mod: %w", err)
	}
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link is not allowed in boundary scan: %s", rel)
		}
		if e.IsDir() && rel != "." && (e.Name() == ".git" || e.Name() == ".artifacts" || e.Name() == ".worktrees" || e.Name() == "node_modules" || e.Name() == "testdata") {
			return filepath.SkipDir
		}
		if !e.IsDir() && e.Name() == "go.mod" {
			kind := "module"
			if rel == "go.mod" {
				out = append(out, Boundary{kind, rel})
			} else {
				out = append(out, Boundary{kind, rel})
			}
		}
		name := strings.ToLower(e.Name())
		var manifestBoundaries []Boundary
		serviceManifest := false
		if !e.IsDir() && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			manifestBoundaries, readErr = yamlServiceBoundaries(rel, data)
			if readErr != nil {
				return fmt.Errorf("decomposition: parse service manifest %s: %w", rel, readErr)
			}
			serviceManifest = len(manifestBoundaries) > 0
		}
		if !e.IsDir() && (name == "procfile" || strings.HasPrefix(name, "dockerfile")) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			sum := sha256.Sum256(data)
			out = append(out, Boundary{"service", rel + "#sha256:" + fmt.Sprintf("%x", sum[:])})
		} else if serviceManifest {
			out = append(out, manifestBoundaries...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("decomposition: scan boundaries: %w", err)
	}
	// A process is an immediate child of cmd/ at each discovered Go module
	// root. This includes nested modules without misclassifying tools/*/cmd.
	modules := append([]Boundary(nil), out...)
	for _, module := range modules {
		if module.Kind != "module" {
			continue
		}
		moduleDir := filepath.Dir(filepath.FromSlash(module.Identity))
		if module.Identity == "go.mod" {
			moduleDir = "."
		}
		cmdRel := filepath.Join(moduleDir, "cmd")
		entries, readErr := os.ReadDir(filepath.Join(root, cmdRel))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("decomposition: read module commands %s: %w", filepath.ToSlash(cmdRel), readErr)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				out = append(out, Boundary{"process", filepath.ToSlash(filepath.Join(cmdRel, entry.Name()))})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Identity < out[j].Identity
	})
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			return nil, fmt.Errorf("decomposition: duplicate observed boundary %s:%s", out[i].Kind, out[i].Identity)
		}
	}
	return out, nil
}

// yamlServiceBoundaries recognizes the deployment formats this gate governs:
// top-level Kubernetes Deployment, StatefulSet, DaemonSet, Job, CronJob, Pod,
// or Service documents, and Docker Compose documents with a services map.
// Provider-specific manifests require an adapter before they are claimed.
func yamlServiceBoundaries(path string, data []byte) ([]Boundary, error) {
	var out []Boundary
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if len(document.Content) == 0 {
			continue
		}
		if err := yamlBoundaryNodes(path, document.Content[0], &out); err != nil {
			return nil, err
		}
	}
}

func yamlBoundaryNodes(path string, node *yaml.Node, out *[]Boundary) error {
	if node == nil {
		return nil
	}
	if err := rejectAmbiguousYAML(node); err != nil {
		return err
	}
	if node.Kind == yaml.SequenceNode {
		for _, item := range node.Content {
			if err := yamlBoundaryNodes(path, item, out); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	fields := yamlFields(node)
	if services, ok := fields["services"]; ok {
		if services.Kind != yaml.MappingNode {
			return fmt.Errorf("compose services must be a mapping")
		}
		for i := 0; i+1 < len(services.Content); i += 2 {
			name := strings.TrimSpace(services.Content[i].Value)
			if name == "" {
				return fmt.Errorf("compose service name is required")
			}
			*out = append(*out, Boundary{"service", path + "#compose/" + name})
		}
		return nil
	}
	if items, ok := fields["items"]; ok && strings.EqualFold(strings.TrimSpace(scalar(fields["kind"])), "list") {
		if items.Kind != yaml.SequenceNode {
			return fmt.Errorf("kubernetes List items must be a sequence")
		}
		for _, item := range items.Content {
			if err := yamlBoundaryNodes(path, item, out); err != nil {
				return err
			}
		}
		return nil
	}
	kind := strings.ToLower(strings.TrimSpace(scalar(fields["kind"])))
	switch kind {
	case "deployment", "statefulset", "daemonset", "job", "cronjob", "pod", "service":
		metadata, ok := fields["metadata"]
		if !ok || metadata.Kind != yaml.MappingNode {
			return fmt.Errorf("kubernetes %s metadata is required", kind)
		}
		mf := yamlFields(metadata)
		name := strings.TrimSpace(scalar(mf["name"]))
		if name == "" {
			return fmt.Errorf("kubernetes %s metadata.name is required", kind)
		}
		ns := strings.TrimSpace(scalar(mf["namespace"]))
		if ns == "" {
			ns = "default"
		}
		*out = append(*out, Boundary{"service", path + "#" + kind + "/" + ns + "/" + name})
	}
	return nil
}

func yamlFields(node *yaml.Node) map[string]*yaml.Node {
	out := make(map[string]*yaml.Node)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.ToLower(strings.TrimSpace(node.Content[i].Value))
		out[key] = node.Content[i+1]
	}
	return out
}

func rejectAmbiguousYAML(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return errors.New("YAML aliases are not allowed in service manifests")
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := strings.ToLower(strings.TrimSpace(node.Content[i].Value))
			if key == "<<" {
				return errors.New("YAML merge keys are not allowed in service manifests")
			}
			if seen[key] {
				return fmt.Errorf("duplicate YAML key %q in service manifest", key)
			}
			seen[key] = true
		}
	}
	for _, child := range node.Content {
		if err := rejectAmbiguousYAML(child); err != nil {
			return err
		}
	}
	return nil
}

func scalar(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func CanonicalBytes(d Decision) ([]byte, error) { return json.Marshal(d) }

func Digest(d Decision) (string, error) {
	b, err := CanonicalBytes(d)
	if err != nil {
		return "", fmt.Errorf("decomposition: canonical decision: %w", err)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

func allowedKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "module", "process", "service":
		return true
	default:
		return false
	}
}

func topologyOnly(s string) bool {
	s = strings.ToLower(s)
	for _, marker := range []string{"team owns", "domain count", "deployment fashion", "one service per", "one process per", "topology"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
