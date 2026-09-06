// Cross-checks in this file read migrations/ and
// definitions/storage/storage-disposition.yaml, and import
// internal/capability, but never write to any of them: DB-016 previews are
// read-only proof that a table name is not already live, never a mutation of
// the live registries.
package deferredschema

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/capability"
	"gopkg.in/yaml.v3"
)

// TableFinding is what ParseMigrationPreview observed about one CREATE
// TABLE block in a preview file.
type TableFinding struct {
	Table                    string
	HasRLSEnable             bool
	HasForceRLS              bool
	HasTenantIsolationPolicy bool
	HasForbidMutationTrigger bool
}

var createTableRe = regexp.MustCompile(`(?m)^CREATE TABLE(?: IF NOT EXISTS)? (\w+)\s*\(`)

// ParseMigrationPreview parses a preview file's -- +goose Up body into one
// TableFinding per CREATE TABLE block, in file order. Only the Up body is
// scanned: the Down body intentionally repeats table names while tearing
// them down, which would otherwise double-count.
func ParseMigrationPreview(previewSQL string) []TableFinding {
	up := UpSQL(previewSQL)
	locs := createTableRe.FindAllStringSubmatchIndex(up, -1)
	findings := make([]TableFinding, 0, len(locs))
	for i, loc := range locs {
		start := loc[0]
		end := len(up)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		block := up[start:end]
		name := up[loc[2]:loc[3]]
		findings = append(findings, TableFinding{
			Table:                    name,
			HasRLSEnable:             strings.Contains(block, "ENABLE ROW LEVEL SECURITY"),
			HasForceRLS:              strings.Contains(block, "FORCE ROW LEVEL SECURITY"),
			HasTenantIsolationPolicy: strings.Contains(block, "CREATE POLICY tenant_isolation ON "+name),
			HasForbidMutationTrigger: strings.Contains(block, "EXECUTE FUNCTION forbid_mutation()"),
		})
	}
	return findings
}

// LiveMigrationTableNames scans every *.sql file directly under
// migrationsDir (migrations/, read-only) and returns the set of table names
// any migration creates.
func LiveMigrationTableNames(migrationsDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("deferredschema: read %s: %w", migrationsDir, err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("deferredschema: read %s: %w", e.Name(), err)
		}
		for _, m := range createTableRe.FindAllStringSubmatch(string(data), -1) {
			names[m[1]] = true
		}
	}
	return names, nil
}

// LiveDispositionTableNames parses definitions/storage/storage-disposition.yaml
// (read-only) and returns the set of registered table names, without
// depending on internal/data/tenancy/storagedisposition's package (this
// package must stay independent of the live registry's loader; see
// disposition.go's DispositionRow doc).
func LiveDispositionTableNames(storageDispositionPath string) (map[string]bool, error) {
	data, err := os.ReadFile(storageDispositionPath)
	if err != nil {
		return nil, fmt.Errorf("deferredschema: read %s: %w", storageDispositionPath, err)
	}
	var doc struct {
		Tables []struct {
			Table string `yaml:"table"`
		} `yaml:"tables"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("deferredschema: parse %s: %w", storageDispositionPath, err)
	}
	names := make(map[string]bool, len(doc.Tables))
	for _, t := range doc.Tables {
		names[t.Table] = true
	}
	return names, nil
}

// CapabilityWriteViolation names a bootstrap capability that would give a
// preview table production write authority.
type CapabilityWriteViolation struct {
	CapabilityID string
	EffectClass  string
	Domain       string
}

// CapabilityRegistryWriteAuthority reports every way the compiled-in Phase 1
// capability registry (internal/capability.NewBootstrapRegistry) could be
// read as exposing authoritative write access to one of previewTables: any
// capability whose effect class can mutate state at all (the registry's own
// EffectClass.IsWrite), and separately, any capability -- regardless of
// effect class -- that lists one of the given domain slugs in its declared
// WriteData. Today NewBootstrapRegistry hands out only EffectReadOnly
// capabilities with empty WriteData, so this returns nothing; the check
// exists so a future write-capable capability naming one of these domains
// fails DB-016's own test rather than shipping silently.
func CapabilityRegistryWriteAuthority(domainSlugs []string) ([]CapabilityWriteViolation, error) {
	reg, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, fmt.Errorf("deferredschema: build bootstrap capability registry: %w", err)
	}
	domainSet := make(map[string]bool, len(domainSlugs))
	for _, s := range domainSlugs {
		domainSet[strings.ToLower(s)] = true
	}

	var violations []CapabilityWriteViolation
	for _, rec := range reg.List() {
		def := rec.Definition
		if def.EffectClass.IsWrite() {
			violations = append(violations, CapabilityWriteViolation{
				CapabilityID: def.ID,
				EffectClass:  string(def.EffectClass),
				Domain:       "(any -- effect class itself is a write)",
			})
			continue
		}
		for _, d := range def.WriteData.DataDomains {
			if domainSet[strings.ToLower(d)] {
				violations = append(violations, CapabilityWriteViolation{
					CapabilityID: def.ID,
					EffectClass:  string(def.EffectClass),
					Domain:       d,
				})
			}
		}
	}
	sort.Slice(violations, func(i, j int) bool { return violations[i].CapabilityID < violations[j].CapabilityID })
	return violations, nil
}

// ValidationReport is the full result of validating one PreviewSet.
type ValidationReport struct {
	// MissingRLS lists tables that never enable+force row-level security
	// with a tenant_isolation policy.
	MissingRLS []string
	// MissingForbidMutation lists append-only (per the disposition preview)
	// tables that never carry the forbid_mutation trigger.
	MissingForbidMutation []string
	// LiveMigrationCollisions lists preview table names that already exist
	// in migrations/.
	LiveMigrationCollisions []string
	// LiveDispositionCollisions lists preview table names that already
	// exist in definitions/storage/storage-disposition.yaml.
	LiveDispositionCollisions []string
	// CapabilityWrites lists ways the Phase 1 capability registry could be
	// read as granting one of these tables/domains authoritative write
	// access.
	CapabilityWrites []CapabilityWriteViolation
	// UnknownDispositionValues lists disposition preview rows whose
	// disposition field is neither DRAFT nor CONFORMANCE.
	UnknownDispositionValues []string
}

// Empty reports whether validation found nothing wrong.
func (r ValidationReport) Empty() bool {
	return len(r.MissingRLS) == 0 &&
		len(r.MissingForbidMutation) == 0 &&
		len(r.LiveMigrationCollisions) == 0 &&
		len(r.LiveDispositionCollisions) == 0 &&
		len(r.CapabilityWrites) == 0 &&
		len(r.UnknownDispositionValues) == 0
}

// Error renders a ValidationReport as a single descriptive error, or nil if
// the report is Empty.
func (r ValidationReport) Error() error {
	if r.Empty() {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "deferredschema: validation failed:")
	if len(r.MissingRLS) > 0 {
		fmt.Fprintf(&b, "\n  missing RLS: %v", r.MissingRLS)
	}
	if len(r.MissingForbidMutation) > 0 {
		fmt.Fprintf(&b, "\n  missing forbid_mutation trigger: %v", r.MissingForbidMutation)
	}
	if len(r.LiveMigrationCollisions) > 0 {
		fmt.Fprintf(&b, "\n  collides with migrations/: %v", r.LiveMigrationCollisions)
	}
	if len(r.LiveDispositionCollisions) > 0 {
		fmt.Fprintf(&b, "\n  collides with definitions/storage/storage-disposition.yaml: %v", r.LiveDispositionCollisions)
	}
	if len(r.CapabilityWrites) > 0 {
		fmt.Fprintf(&b, "\n  capability registry write authority: %+v", r.CapabilityWrites)
	}
	if len(r.UnknownDispositionValues) > 0 {
		fmt.Fprintf(&b, "\n  unknown disposition values: %v", r.UnknownDispositionValues)
	}
	return fmt.Errorf("%s", b.String())
}

// Validate is the whole DB-016 validator: it parses every migration preview
// in p, asserts every table has RLS and every append-only table (per the
// disposition preview) also has a forbid_mutation trigger, and cross-checks
// preview table names against migrations/, definitions/storage/
// storage-disposition.yaml and the Phase 1 capability registry. root is the
// repository root (so migrationsDir and storageDispositionPath can be
// resolved); domains is the domain catalog the preview set was generated
// from (Domains()).
func Validate(root string, domains []Domain, p PreviewSet) (ValidationReport, error) {
	var report ValidationReport

	dispositionRaw, ok := p.Lookup("storage-disposition.deferred.yaml")
	if !ok {
		return report, fmt.Errorf("deferredschema: preview set has no storage-disposition.deferred.yaml")
	}
	rows, err := LoadDispositionPreviewYAML([]byte(dispositionRaw))
	if err != nil {
		return report, err
	}
	appendOnly := make(map[string]bool, len(rows))
	allTables := make([]string, 0, len(rows))
	for _, row := range rows {
		appendOnly[row.Table] = row.AppendOnly
		allTables = append(allTables, row.Table)
		if row.Disposition != "DRAFT" && row.Disposition != "CONFORMANCE" {
			report.UnknownDispositionValues = append(report.UnknownDispositionValues, fmt.Sprintf("%s=%s", row.Table, row.Disposition))
		}
	}

	for _, f := range p.Files {
		if !strings.HasSuffix(f.Name, ".sql") {
			continue
		}
		for _, finding := range ParseMigrationPreview(f.Content) {
			if !(finding.HasRLSEnable && finding.HasForceRLS && finding.HasTenantIsolationPolicy) {
				report.MissingRLS = append(report.MissingRLS, finding.Table)
			}
			if appendOnly[finding.Table] && !finding.HasForbidMutationTrigger {
				report.MissingForbidMutation = append(report.MissingForbidMutation, finding.Table)
			}
		}
	}

	liveMigrations, err := LiveMigrationTableNames(filepath.Join(root, "migrations"))
	if err != nil {
		return report, err
	}
	liveDisposition, err := LiveDispositionTableNames(filepath.Join(root, "definitions", "storage", "storage-disposition.yaml"))
	if err != nil {
		return report, err
	}
	for _, name := range allTables {
		if liveMigrations[name] {
			report.LiveMigrationCollisions = append(report.LiveMigrationCollisions, name)
		}
		if liveDisposition[name] {
			report.LiveDispositionCollisions = append(report.LiveDispositionCollisions, name)
		}
	}

	slugs := make([]string, 0, len(domains))
	for _, d := range domains {
		slugs = append(slugs, d.Slug)
	}
	violations, err := CapabilityRegistryWriteAuthority(slugs)
	if err != nil {
		return report, err
	}
	report.CapabilityWrites = violations

	sort.Strings(report.MissingRLS)
	sort.Strings(report.MissingForbidMutation)
	sort.Strings(report.LiveMigrationCollisions)
	sort.Strings(report.LiveDispositionCollisions)
	sort.Strings(report.UnknownDispositionValues)

	return report, nil
}
