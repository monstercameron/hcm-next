// Package placementbindings implements ALIGN-012/ALIGN-014's SQL-side
// binding checks. It proves tenant columns, tenant isolation policy, placement
// anchors, and domain-default metadata from the versioned migration text.
package placementbindings

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/tools/policy/tableinventory"
)

const schemaVersion = 1

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// TableSchema is the migration-observable part of one default-schema table.
type TableSchema struct {
	Table        string            `json:"table"`
	Migration    string            `json:"migration"`
	Columns      map[string]string `json:"columns"`
	Policies     map[string]bool   `json:"policies"`
	ForceRLS     bool              `json:"force_rls"`
	Declarations string            `json:"-"`
}

// Default is one SQL DEFAULT attached to a column.
type Default struct {
	Table      string `json:"table"`
	Column     string `json:"column"`
	Expression string `json:"expression"`
	Migration  string `json:"migration"`
}

// Finding is one tenant, placement, or default-semantics violation.
type Finding struct {
	Table  string `json:"table"`
	Column string `json:"column,omitempty"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (f Finding) Error() string {
	if f.Column != "" {
		return fmt.Sprintf("placementbindings: %s.%s: %s: %s", f.Table, f.Column, f.Code, f.Detail)
	}
	return fmt.Sprintf("placementbindings: %s: %s: %s", f.Table, f.Code, f.Detail)
}

// Report contains every finding, preserving all deficient tables for repair.
type Report struct {
	Findings []Finding     `json:"findings"`
	Schemas  []TableSchema `json:"schemas"`
}

func (r Report) OK() bool { return len(r.Findings) == 0 }

func (r Report) Explain() string {
	return fmt.Sprintf("placement binding report v%d with %d schema(s) and %d finding(s)", schemaVersion, len(r.Schemas), len(r.Findings))
}

// Evaluate reads the source storage registry and migrations under root.
func Evaluate(root string) (Report, error) {
	inventory, err := tableinventory.Scan(root)
	if err != nil {
		return Report{}, err
	}
	schemas, err := ScanMigrations(filepath.Join(root, "migrations"))
	if err != nil {
		return Report{}, fmt.Errorf("placementbindings: scan migrations: %w", err)
	}
	allowed := make(map[string]bool, len(inventory.SourceFiles))
	for _, file := range inventory.SourceFiles {
		allowed[file] = true
	}
	filtered := schemas[:0]
	for _, schema := range schemas {
		if allowed[schema.Migration] {
			filtered = append(filtered, schema)
		}
	}
	schemas = filtered
	findings := Validate(inventory.Tables, schemas)
	findings = append(findings, ValidateDefaults(schemas)...)
	return Report{Findings: sortFindings(findings), Schemas: schemas}, nil
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

// Validate applies the policy to supplied registry entries and migration
// schemas. It is pure and returns all findings in stable order.
func Validate(tables []tableinventory.Table, schemas []TableSchema) []Finding {
	byName := make(map[string]TableSchema, len(schemas))
	for _, schema := range schemas {
		byName[strings.ToLower(schema.Table)] = schema
	}
	var findings []Finding
	for _, table := range tables {
		if !table.TenantScoped() {
			continue
		}
		name := table.Table
		schema, ok := byName[strings.ToLower(name)]
		if !ok {
			findings = append(findings, Finding{Table: name, Code: "MISSING_SCHEMA", Detail: "tenant-scoped table is not declared by migrations"})
			continue
		}
		column := strings.ToLower(strings.TrimSpace(*table.TenantScopingColumn))
		definition, exists := schema.Columns[column]
		if !exists {
			findings = append(findings, Finding{Table: name, Code: "TENANT_COLUMN_MISSING", Detail: fmt.Sprintf("declared tenant_scoping_column %q is absent", column)})
		} else if !strings.Contains(strings.ToUpper(definition), "NOT NULL") {
			findings = append(findings, Finding{Table: name, Code: "TENANT_COLUMN_NULLABLE", Detail: fmt.Sprintf("declared tenant_scoping_column %q is not NOT NULL", column)})
		}
		if !schema.Policies["tenant_isolation"] {
			findings = append(findings, Finding{Table: name, Code: "TENANT_POLICY_MISSING", Detail: "tenant-scoped table has no tenant_isolation policy"})
		}
		if !schema.ForceRLS {
			findings = append(findings, Finding{Table: name, Code: "FORCE_RLS_MISSING", Detail: "tenant-scoped table does not FORCE ROW LEVEL SECURITY"})
		}
	}
	findings = append(findings, validatePlacementAnchor(byName)...)
	return sortFindings(findings)
}

// ValidateDefaults rejects SQL defaults on business columns. Timestamp and
// coordination metadata defaults are infrastructure semantics and are
// permitted; domain values such as status, state, amount, and names must be
// supplied by the business contract rather than invented by PostgreSQL.
func ValidateDefaults(schemas []TableSchema) []Finding {
	var findings []Finding
	for _, schema := range schemas {
		for column, definition := range schema.Columns {
			upper := strings.ToUpper(definition)
			marker := strings.Index(upper, " DEFAULT ")
			if marker < 0 || technicalDefaultColumn(column) {
				continue
			}
			expression := strings.TrimSpace(definition[marker+len(" DEFAULT "):])
			findings = append(findings, Finding{
				Table: schema.Table, Column: column, Code: "DOMAIN_DEFAULT_UNAUTHORIZED",
				Detail: fmt.Sprintf("%s.%s has SQL DEFAULT %q; the business contract must supply domain values", schema.Table, column, expression),
			})
		}
	}
	return sortFindings(findings)
}

func technicalDefaultColumn(column string) bool {
	column = strings.ToLower(column)
	if strings.HasSuffix(column, "_at") || column == "created_at" || column == "updated_at" || column == "recorded_at" || column == "applied_at" || column == "processed_at" {
		return true
	}
	switch column {
	case "row_id", "event_id", "journal_id", "created_by", "version", "instance_version", "generation", "fence", "cell_epoch", "event_sequence", "sequence", "recorded_sequence":
		return true
	default:
		return false
	}
}

func validatePlacementAnchor(schemas map[string]TableSchema) []Finding {
	anchor, ok := schemas["tenant_placement"]
	if !ok {
		return []Finding{{Table: "tenant_placement", Code: "PLACEMENT_ANCHOR_MISSING", Detail: "tenant placement table is absent from migrations"}}
	}
	var findings []Finding
	for _, column := range []string{"tenant_id", "cell", "epoch", "signature"} {
		if _, ok := anchor.Columns[column]; !ok {
			findings = append(findings, Finding{Table: anchor.Table, Code: "PLACEMENT_COLUMN_MISSING", Detail: fmt.Sprintf("placement anchor lacks %s", column)})
		}
	}
	if !anchor.Policies["tenant_isolation"] {
		findings = append(findings, Finding{Table: anchor.Table, Code: "TENANT_POLICY_MISSING", Detail: "placement anchor has no tenant_isolation policy"})
	}
	if !anchor.ForceRLS {
		findings = append(findings, Finding{Table: anchor.Table, Code: "FORCE_RLS_MISSING", Detail: "placement anchor does not FORCE ROW LEVEL SECURITY"})
	}
	return findings
}

// ScanMigrations extracts default-schema CREATE TABLE columns and the
// tenant_isolation/FORCE RLS declarations. Companion artifact schemas are
// intentionally not part of this default-table policy.
func ScanMigrations(migrationsDir string) ([]TableSchema, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, err
	}
	var schemas []TableSchema
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		text := string(body)
		for _, match := range createTablePattern.FindAllStringSubmatchIndex(text, -1) {
			schemaName := ""
			if match[2] >= 0 {
				schemaName = text[match[2]:match[3]]
			}
			if schemaName != "" && !strings.EqualFold(schemaName, "public") {
				continue
			}
			name := strings.ToLower(text[match[4]:match[5]])
			open := match[6]
			close := strings.Index(text[open+1:], ");")
			if close < 0 {
				continue
			}
			bodyText := text[open+1 : open+1+close]
			columns := parseColumns(bodyText)
			policyPattern := regexp.MustCompile(`(?im)CREATE\s+POLICY\s+(\w+)\s+ON\s+` + regexp.QuoteMeta(name) + `\b`)
			policies := make(map[string]bool)
			for _, policy := range policyPattern.FindAllStringSubmatch(text, -1) {
				policies[strings.ToLower(policy[1])] = true
			}
			force := regexp.MustCompile(`(?im)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?` + regexp.QuoteMeta(name) + `\s+FORCE\s+ROW\s+LEVEL\s+SECURITY`).MatchString(text)
			schemas = append(schemas, TableSchema{Table: name, Migration: entry.Name(), Columns: columns, Policies: policies, ForceRLS: force, Declarations: bodyText})
		}
	}
	sort.Slice(schemas, func(i, j int) bool {
		if schemas[i].Table != schemas[j].Table {
			return schemas[i].Table < schemas[j].Table
		}
		return schemas[i].Migration < schemas[j].Migration
	})
	return schemas, nil
}

var createTablePattern = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(\w+)\.)?(\w+)\s*(\()`)

func parseColumns(body string) map[string]string {
	columns := make(map[string]string)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, ","))
		if line == "" || strings.HasPrefix(line, "--") || strings.HasPrefix(strings.ToUpper(line), "CONSTRAINT ") || strings.HasPrefix(strings.ToUpper(line), "PRIMARY KEY") || strings.HasPrefix(strings.ToUpper(line), "UNIQUE ") || strings.HasPrefix(strings.ToUpper(line), "CHECK ") || strings.HasPrefix(strings.ToUpper(line), "FOREIGN KEY") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !identifierPattern.MatchString(fields[0]) {
			continue
		}
		columns[strings.ToLower(strings.Trim(fields[0], `"`))] = line
	}
	return columns
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func sortFindings(findings []Finding) []Finding {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Table != findings[j].Table {
			return findings[i].Table < findings[j].Table
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}
