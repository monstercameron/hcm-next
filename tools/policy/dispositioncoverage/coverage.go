// Package dispositioncoverage implements the DB-COVERAGE-001 policy:
// ensuring that every model object (entity, property, relationship) has
// either a database disposition or an explicit non-database disposition entry.
//
// The coverage checker loads:
// - definitions/model/storage-disposition.yaml: auto-generated entity dispositions (DB-002 output)
// - definitions/storage/storage-disposition.yaml: physical table registry (STORE-001)
// - A registry of approved non-database dispositions (e.g. VALUE_OBJECT, TRANSIENT, COMPUTED)
//
// Every entity must have either:
// - disposition != MISMATCH in the model manifest, OR
// - An explicit entry in the non-database disposition registry
//
// This ensures no model object is accidentally orphaned or undeclared.
package dispositioncoverage

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// EntityDispositionEntry is one row of the model storage-disposition.yaml entities section.
type EntityDispositionEntry struct {
	EntityRef      string `yaml:"entityref"`
	Key            string `yaml:"key"`
	Owner          string `yaml:"owner"`
	Class          string `yaml:"class"`
	Disposition    string `yaml:"disposition"`
	Target         string `yaml:"target"`
	MismatchDetail string `yaml:"mismatchdetail"`
}

// ModelDispositionManifest is the parsed form of definitions/model/storage-disposition.yaml.
type ModelDispositionManifest struct {
	GeneratedBy    string                   `yaml:"generatedby"`
	RegistryDigest string                   `yaml:"registrydigest"`
	MigrationFiles []string                 `yaml:"migrationfiles"`
	Entities       []EntityDispositionEntry `yaml:"entities"`
}

// NonDatabaseDisposition is an explicit entry for a model object that does not
// have a physical database disposition.
type NonDatabaseDisposition struct {
	EntityRef  string `yaml:"entityref"`
	Kind       string `yaml:"kind"` // VALUE_OBJECT, TRANSIENT, COMPUTED, etc.
	Reason     string `yaml:"reason"`
	ApprovedBy string `yaml:"approved_by"`
}

// NonDatabaseRegistry is the parsed form of a registry of approved non-database dispositions.
type NonDatabaseRegistry struct {
	Dispositions []NonDatabaseDisposition `yaml:"dispositions"`
}

// LoadModelDisposition reads and parses definitions/model/storage-disposition.yaml.
func LoadModelDisposition(path string) (*ModelDispositionManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dispositioncoverage: reading model manifest: %w", err)
	}
	var m ModelDispositionManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("dispositioncoverage: parsing model manifest: %w", err)
	}
	return &m, nil
}

// LoadNonDatabaseRegistry reads and parses a non-database disposition registry.
func LoadNonDatabaseRegistry(path string) (*NonDatabaseRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dispositioncoverage: reading non-database registry: %w", err)
	}
	var r NonDatabaseRegistry
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("dispositioncoverage: parsing non-database registry: %w", err)
	}
	return &r, nil
}

// CoverageGap is one uncovered model object.
type CoverageGap struct {
	EntityRef string
	Reason    string
}

// CheckCoverage verifies that every entity in the model manifest has either
// a database disposition (disposition != "MISMATCH") or an explicit non-database entry.
// It returns a sorted list of gaps (entities with neither).
func CheckCoverage(modelManifest *ModelDispositionManifest, nonDatabaseRegistry *NonDatabaseRegistry) []CoverageGap {
	// Build the non-database set for O(1) lookups.
	nonDbSet := make(map[string]bool, len(nonDatabaseRegistry.Dispositions))
	for _, e := range nonDatabaseRegistry.Dispositions {
		nonDbSet[e.EntityRef] = true
	}

	// Check each entity.
	var gaps []CoverageGap
	for _, entity := range modelManifest.Entities {
		// MISMATCH means no database disposition (needs a migration).
		// Non-MISMATCH means it has a physical disposition (TABLE, EVENT, ARTIFACT, PROJECTION, VALUE_OBJECT).
		if entity.Disposition != "MISMATCH" {
			// Has a database disposition; covered.
			continue
		}

		// MISMATCH: check if it has an explicit non-database entry.
		if nonDbSet[entity.EntityRef] {
			// Has an explicit non-database disposition; covered.
			continue
		}

		// Neither: gap.
		gaps = append(gaps, CoverageGap{
			EntityRef: entity.EntityRef,
			Reason:    entity.MismatchDetail,
		})
	}

	// Sort for stable output.
	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].EntityRef < gaps[j].EntityRef
	})

	return gaps
}

// CoverageReport summarizes the coverage check results.
type CoverageReport struct {
	TotalEntities    int
	WithDatabasePlan int
	WithNonDatabase  int
	Uncovered        []CoverageGap
}

// GenerateReport produces a report of the coverage check.
func GenerateReport(modelManifest *ModelDispositionManifest, nonDatabaseRegistry *NonDatabaseRegistry) *CoverageReport {
	gaps := CheckCoverage(modelManifest, nonDatabaseRegistry)

	// Build the non-database set.
	nonDbSet := make(map[string]bool, len(nonDatabaseRegistry.Dispositions))
	for _, e := range nonDatabaseRegistry.Dispositions {
		nonDbSet[e.EntityRef] = true
	}

	// Count database dispositions.
	withDb := 0
	for _, entity := range modelManifest.Entities {
		if entity.Disposition != "MISMATCH" {
			withDb++
		}
	}

	return &CoverageReport{
		TotalEntities:    len(modelManifest.Entities),
		WithDatabasePlan: withDb,
		WithNonDatabase:  len(nonDatabaseRegistry.Dispositions),
		Uncovered:        gaps,
	}
}
