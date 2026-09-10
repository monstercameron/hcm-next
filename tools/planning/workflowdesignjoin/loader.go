// Live snapshot loading for the design join: accepted definitions from
// the intent-conformance descriptors (shared by import with
// tools/planning/intentcoverage/GOV-026, never re-derived) and design
// records from the workflowdesign sidecar. Records must validate clean
// first: an INVALID_DEFINITION never reaches the join.
package workflowdesignjoin

import (
	"fmt"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
)

// LoadSnapshot reads the live accepted definitions and design records
// below root. It fails closed when the records violate the design
// contract.
func LoadSnapshot(root string) ([]string, []workflowdesign.DesignRecord, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve repository root: %w", err)
	}
	descriptors, err := intentmanifests.LoadIntentManifestYAML(filepath.Join(root, "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if err != nil {
		return nil, nil, err
	}
	if err := intentmanifests.ValidateIntentManifestYAML(descriptors); err != nil {
		return nil, nil, fmt.Errorf("accepted intent catalog failed validation: %w", err)
	}
	var accepted []string
	for _, d := range descriptors {
		accepted = append(accepted, fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version))
	}
	records, err := workflowdesign.LoadRecords(filepath.Join(root, "tools", "planning", "workflowdesign", "testdata", "seed", "records.yaml"))
	if err != nil {
		return nil, nil, err
	}
	if report := workflowdesign.ValidateRecords(records); !report.OK() {
		return nil, nil, fmt.Errorf("design records violate the contract: %+v", report.Findings)
	}
	return accepted, records, nil
}
