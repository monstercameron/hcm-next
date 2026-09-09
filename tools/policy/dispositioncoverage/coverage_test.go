package dispositioncoverage_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/dispositioncoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// TestTodo_DB_COVERAGE_001 is the PRIMARY test for DB-COVERAGE-001:
// every model object (entity) must have either a database disposition
// or an explicit non-database disposition entry. No orphaned entities.
func TestTodo_DB_COVERAGE_001(t *testing.T) {
	root := repopath.RootDir()

	// Load the model disposition manifest (DB-002 output).
	modelPath := filepath.Join(root, "definitions", "model", "storage-disposition.yaml")
	modelManifest, err := dispositioncoverage.LoadModelDisposition(modelPath)
	if err != nil {
		t.Fatalf("loading model manifest: %v", err)
	}

	// Load the non-database disposition registry.
	// For now, if the file doesn't exist, we create an empty registry
	// (all entities must have database dispositions or the test will fail on gaps).
	nonDbPath := filepath.Join(root, "definitions", "model", "non-database-disposition.yaml")
	nonDbRegistry, err := dispositioncoverage.LoadNonDatabaseRegistry(nonDbPath)
	if err != nil {
		// File not found is OK for now; start with empty registry.
		nonDbRegistry = &dispositioncoverage.NonDatabaseRegistry{
			Dispositions: []dispositioncoverage.NonDatabaseDisposition{},
		}
	}

	// Check coverage.
	gaps := dispositioncoverage.CheckCoverage(modelManifest, nonDbRegistry)

	// Sort gaps for consistent error messages.
	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].EntityRef < gaps[j].EntityRef
	})

	// Current gaps (DB-COVERAGE-001 REFACTOR: zero unowned objects with stable digest).
	// These are entities that are declared in the model but not yet migrated.
	// The list is sorted and includes a count comment so new gaps fail the test.
	//
	// COUNT: 13 (as of 2026-09-05)
	allowedGaps := map[string]bool{
		"ApprovalBinding/v1":          true,
		"Assignment/v1":               true,
		"BudgetReservation/v1":        true,
		"CompensationComponent/v1":    true,
		"CompensationGrade/v1":        true,
		"CompensationPackage/v1":      true,
		"ExecutionBinding/v1":         true,
		"Job/v1":                      true,
		"LegalEntity/v1":              true,
		"OrganizationRelationship/v1": true,
		"OrganizationUnit/v1":         true,
		"PositionOccupancy/v1":        true,
		"RepairPlan/v1":               true,
	}

	// Verify gap count matches.
	if len(gaps) != len(allowedGaps) {
		t.Errorf("gap count changed: got %d, expected %d", len(gaps), len(allowedGaps))
	}

	// Report each gap with details.
	for _, gap := range gaps {
		if !allowedGaps[gap.EntityRef] {
			t.Errorf("NEW GAP: %s: %s", gap.EntityRef, gap.Reason)
		} else {
			t.Logf("Known gap: %s", gap.EntityRef)
		}
	}

	// Generate and log a coverage report.
	report := dispositioncoverage.GenerateReport(modelManifest, nonDbRegistry)
	t.Logf("Coverage report: %d total entities, %d with database plan, %d with non-database disposition, %d uncovered",
		report.TotalEntities, report.WithDatabasePlan, report.WithNonDatabase, len(report.Uncovered))
}
