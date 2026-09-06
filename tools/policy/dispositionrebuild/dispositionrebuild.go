// Package dispositionrebuild implements ALIGN-015. It verifies that every
// REBUILDABLE storage disposition names a real append-only ledger source and
// that the source is present in the versioned default-schema migrations.
package dispositionrebuild

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/data/tenancy/storagedisposition"
	"github.com/monstercameron/hcm-next/tools/policy/tableinventory"
)

const schemaVersion = 1

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// Finding is one storage disposition or rebuild-source violation.
type Finding struct {
	Table  string `json:"table"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (f Finding) Error() string {
	return fmt.Sprintf("dispositionrebuild: %s: %s: %s", f.Table, f.Code, f.Detail)
}

// RebuildPlan is the safe, bounded projection of rebuild sources.
type RebuildPlan struct {
	Table  string `json:"table"`
	Source string `json:"source"`
	Role   string `json:"role"`
}

// Report contains all findings and all accepted rebuild plans.
type Report struct {
	Findings []Finding     `json:"findings"`
	Plans    []RebuildPlan `json:"plans"`
}

func (r Report) OK() bool { return len(r.Findings) == 0 }

func (r Report) Explain() string {
	return fmt.Sprintf("storage disposition report v%d with %d rebuild plan(s) and %d finding(s)", schemaVersion, len(r.Plans), len(r.Findings))
}

// Evaluate loads and checks the source registry and migration inventory.
func Evaluate(root string) (Report, error) {
	inventory, err := tableinventory.Scan(root)
	if err != nil {
		return Report{}, err
	}
	findings, plans := Validate(inventory.Tables, inventory.MigrationTables)
	return Report{Findings: findings, Plans: plans}, nil
}

// Check is the policy-tool entry point.
func Check(root string) error {
	report, err := Evaluate(root)
	if err != nil {
		return err
	}
	if !report.OK() {
		parts := make([]string, len(report.Findings))
		for i, finding := range report.Findings {
			parts[i] = finding.Error()
		}
		return fmt.Errorf("%s", strings.Join(parts, "; "))
	}
	return nil
}

// Validate applies the rebuild contract to supplied entries and physical
// table names. It returns all findings and all valid plans, sorted.
func Validate(tables []tableinventory.Table, migrationTables []tableinventory.MigrationTable) ([]Finding, []RebuildPlan) {
	registered := make(map[string]tableinventory.Table, len(tables))
	for _, table := range tables {
		registered[strings.ToLower(table.Table)] = table
	}
	physical := make(map[string]bool, len(migrationTables))
	for _, table := range migrationTables {
		physical[strings.ToLower(table.Name)] = true
	}
	var findings []Finding
	var plans []RebuildPlan
	for _, table := range tables {
		if table.RetentionClass != storagedisposition.RetentionRebuildable {
			continue
		}
		if table.RebuildSource == nil || strings.TrimSpace(*table.RebuildSource) == "" {
			findings = append(findings, Finding{Table: table.Table, Code: "REBUILD_SOURCE_MISSING", Detail: "REBUILDABLE table has no rebuild_source"})
			continue
		}
		sourceName := strings.TrimSpace(*table.RebuildSource)
		if sourceName == table.Table {
			findings = append(findings, Finding{Table: table.Table, Code: "REBUILD_SOURCE_SELF", Detail: "rebuild_source names the table itself"})
			continue
		}
		source, ok := registered[strings.ToLower(sourceName)]
		if !ok {
			findings = append(findings, Finding{Table: table.Table, Code: "REBUILD_SOURCE_UNREGISTERED", Detail: fmt.Sprintf("rebuild_source %q is not a registered table", sourceName)})
			continue
		}
		if source.DataRole != storagedisposition.RoleLedger {
			findings = append(findings, Finding{Table: table.Table, Code: "REBUILD_SOURCE_NOT_LEDGER", Detail: fmt.Sprintf("rebuild_source %q has role %q, want %q", sourceName, source.DataRole, storagedisposition.RoleLedger)})
			continue
		}
		if !physical[strings.ToLower(sourceName)] {
			findings = append(findings, Finding{Table: table.Table, Code: "REBUILD_SOURCE_NOT_MIGRATED", Detail: fmt.Sprintf("rebuild_source %q is registered but absent from migrations", sourceName)})
			continue
		}
		plans = append(plans, RebuildPlan{Table: table.Table, Source: sourceName, Role: source.DataRole})
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Table != findings[j].Table {
			return findings[i].Table < findings[j].Table
		}
		return findings[i].Code < findings[j].Code
	})
	sort.Slice(plans, func(i, j int) bool { return plans[i].Table < plans[j].Table })
	return findings, plans
}
