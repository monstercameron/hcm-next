// Command dispositioncoverage reports the coverage status of all model entities
// against the storage disposition registries. Part of DB-COVERAGE-001.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/dispositioncoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root := repopath.RootDir()

	// Load the model disposition manifest (DB-002 output).
	modelPath := filepath.Join(root, "definitions", "model", "storage-disposition.yaml")
	modelManifest, err := dispositioncoverage.LoadModelDisposition(modelPath)
	if err != nil {
		return fmt.Errorf("loading model manifest: %w", err)
	}

	// Load the non-database disposition registry.
	nonDbPath := filepath.Join(root, "definitions", "model", "non-database-disposition.yaml")
	nonDbRegistry, err := dispositioncoverage.LoadNonDatabaseRegistry(nonDbPath)
	if err != nil {
		// File not found is OK; start with empty registry.
		nonDbRegistry = &dispositioncoverage.NonDatabaseRegistry{
			Dispositions: []dispositioncoverage.NonDatabaseDisposition{},
		}
	}

	// Generate report.
	report := dispositioncoverage.GenerateReport(modelManifest, nonDbRegistry)

	// Print findings.
	fmt.Printf("DB-COVERAGE-001 Disposition Coverage Report\n")
	fmt.Printf("=============================================\n\n")

	fmt.Printf("Summary:\n")
	fmt.Printf("  Total entities: %d\n", report.TotalEntities)
	fmt.Printf("  With database disposition: %d\n", report.WithDatabasePlan)
	fmt.Printf("  With non-database disposition: %d\n", report.WithNonDatabase)
	fmt.Printf("  Uncovered: %d\n\n", len(report.Uncovered))

	if len(report.Uncovered) > 0 {
		fmt.Printf("Uncovered entities (disposition required):\n")
		for _, gap := range report.Uncovered {
			fmt.Printf("  - %s\n", gap.EntityRef)
			if gap.Reason != "" {
				fmt.Printf("    Reason: %s\n", gap.Reason)
			}
		}
	} else {
		fmt.Printf("No uncovered entities.\n")
	}

	return nil
}
