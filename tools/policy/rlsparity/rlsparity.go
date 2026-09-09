// Package rlsparity implements ALIGN-013. It joins the storage-disposition
// registry, versioned row-level-security declarations, and the repository
// SQL-scope check into one deterministic parity report.
//
// RLS is the database backstop and repository scope is defense in depth. A
// green report therefore requires both sides to name the same tenant
// boundary. The existing STORE-002 reviewed exceptions are retained as
// explicit evidence; they are not added to or widened by this package.
package rlsparity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/storeboundaries"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory"
)

const schemaVersion = 1

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// Explain describes the bounded parity contract without exposing source text.
func Explain() string {
	return fmt.Sprintf("row-security and repository-scope parity policy v%d", schemaVersion)
}

// RLSBinding is the migration-observable security contract for one registered
// tenant-scoped table. PolicyMigration identifies the source declaration, not
// a generated or live-schema artifact.
type RLSBinding struct {
	Table           string `json:"table"`
	TenantColumn    string `json:"tenant_column"`
	PolicyMigration string `json:"policy_migration"`
	Enabled         bool   `json:"enabled"`
	Forced          bool   `json:"forced"`
	Policy          bool   `json:"tenant_isolation_policy"`
	UsingTenant     bool   `json:"using_tenant"`
	WithCheckTenant bool   `json:"with_check_tenant"`
	SessionTenant   bool   `json:"session_tenant"`
}

// RepositoryScope is one repository statement's tenant-scope disposition.
// Scoped is true for source evidence that explicitly carries a tenant
// predicate or a reviewed, bounded transaction-scope contract.
type RepositoryScope struct {
	Table        string `json:"table"`
	TenantColumn string `json:"tenant_column,omitempty"`
	Package      string `json:"package"`
	File         string `json:"file,omitempty"`
	Function     string `json:"function,omitempty"`
	Scoped       bool   `json:"scoped"`
	Disposition  string `json:"disposition"`
}

const (
	DispositionVerified          = "VERIFIED"
	DispositionReviewedException = "REVIEWED_EXCEPTION"
	DispositionUnproven          = "UNPROVEN"
)

// Finding is one parity violation.
type Finding struct {
	Table    string `json:"table"`
	Package  string `json:"package,omitempty"`
	File     string `json:"file,omitempty"`
	Function string `json:"function,omitempty"`
	Code     string `json:"code"`
	Detail   string `json:"detail"`
}

func (f Finding) Error() string {
	location := f.Table
	if f.Package != "" {
		location += " (" + f.Package
		if f.File != "" {
			location += "/" + f.File
		}
		if f.Function != "" {
			location += ":" + f.Function
		}
		location += ")"
	}
	return fmt.Sprintf("rlsparity: %s: %s: %s", location, f.Code, f.Detail)
}

// Report contains migration evidence, repository evidence, and all
// unreviewed parity findings. Reviewed STORE-002 gaps remain visible in
// RepositoryScopes with DispositionReviewedException.
type Report struct {
	Findings           []Finding         `json:"findings"`
	RLS                []RLSBinding      `json:"rls"`
	RepositoryScopes   []RepositoryScope `json:"repository_scopes"`
	ReviewedExceptions int               `json:"reviewed_exceptions"`
}

// OK reports whether no unreviewed parity finding remains.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Explain renders bounded structural evidence suitable for logs and release
// evidence. It never includes table contents or SQL source.
func (r Report) Explain() string {
	return fmt.Sprintf("%s: %d RLS binding(s), %d repository scope(s), %d reviewed exception(s), %d finding(s)", Explain(), len(r.RLS), len(r.RepositoryScopes), r.ReviewedExceptions, len(r.Findings))
}

// Evaluate loads the authoritative storage registry, scans all migration
// files for RLS declarations, and joins the reviewed STORE-002 repository
// scope evidence. It never connects to PostgreSQL and never writes output.
func Evaluate(root string) (Report, error) {
	inventory, err := tableinventory.Scan(root)
	if err != nil {
		return Report{}, err
	}
	rls, err := ScanRLS(filepath.Join(root, "migrations"), inventory.Tables)
	if err != nil {
		return Report{}, fmt.Errorf("rlsparity: scan RLS: %w", err)
	}

	storeReport, err := storeboundaries.Scan(root)
	if err != nil {
		return Report{}, fmt.Errorf("rlsparity: scan repository scope: %w", err)
	}
	policy, err := storeboundaries.LoadPolicy(filepath.Join(root, "tools", "policy", "storeboundaries", "allowlist.yaml"))
	if err != nil {
		return Report{}, fmt.Errorf("rlsparity: load repository-scope review: %w", err)
	}
	remaining := storeboundaries.CheckWithPolicy(storeReport.Violations(), policy)
	remainingSet := make(map[string]bool, len(remaining))
	for _, finding := range remaining {
		remainingSet[finding.String()] = true
	}

	var scopes []RepositoryScope
	for _, finding := range storeReport.TenantScope {
		violation := storeboundaries.Violation{
			Kind: storeboundaries.KindTenantScope, Package: finding.Package,
			File: finding.File, Func: finding.Func, Message: finding.String(),
		}
		reviewed := !remainingSet[violation.String()]
		for _, table := range finding.Tables {
			scope := RepositoryScope{
				Table: table, TenantColumn: tenantColumn(inventory.Tables, table),
				Package: finding.Package, File: finding.File, Function: finding.Func,
				Scoped:      reviewed,
				Disposition: DispositionUnproven,
			}
			if reviewed {
				scope.Disposition = DispositionReviewedException
			}
			scopes = append(scopes, scope)
		}
	}

	findings := Validate(inventory.Tables, rls, scopes)
	return Report{
		Findings: findings, RLS: rls, RepositoryScopes: scopes,
		ReviewedExceptions: countReviewed(scopes),
	}, nil
}

// Check is the policy-tool entry point. Reviewed STORE-002 exceptions are
// accepted only as the existing, complete, expiring review records loaded by
// storeboundaries; new repository gaps remain errors.
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
		return errors.New(strings.Join(parts, "; "))
	}
	return nil
}

// Validate applies parity rules to caller-supplied migration and repository
// evidence. It is pure and returns every finding in stable order.
func Validate(tables []tableinventory.Table, rls []RLSBinding, repositories []RepositoryScope) []Finding {
	rlsByTable := make(map[string]RLSBinding, len(rls))
	for _, binding := range rls {
		rlsByTable[strings.ToLower(strings.TrimSpace(binding.Table))] = binding
	}
	tenantTables := make(map[string]tableinventory.Table)
	var findings []Finding
	for _, table := range tables {
		if !table.TenantScoped() {
			continue
		}
		name := strings.TrimSpace(table.Table)
		key := strings.ToLower(name)
		tenantTables[key] = table
		binding, ok := rlsByTable[key]
		if !ok {
			findings = append(findings, Finding{Table: name, Code: "RLS_MISSING", Detail: "tenant-scoped table has no migration RLS evidence"})
			continue
		}
		if !binding.Enabled {
			findings = append(findings, Finding{Table: name, Code: "RLS_DISABLED", Detail: "tenant-scoped table does not ENABLE ROW LEVEL SECURITY"})
		}
		if !binding.Forced {
			findings = append(findings, Finding{Table: name, Code: "FORCE_RLS_MISSING", Detail: "tenant-scoped table does not FORCE ROW LEVEL SECURITY"})
		}
		if !binding.Policy {
			findings = append(findings, Finding{Table: name, Code: "TENANT_POLICY_MISSING", Detail: "tenant-scoped table has no tenant_isolation policy"})
			continue
		}
		wantColumn := strings.ToLower(strings.TrimSpace(*table.TenantScopingColumn))
		if strings.ToLower(strings.TrimSpace(binding.TenantColumn)) != wantColumn {
			findings = append(findings, Finding{Table: name, Code: "RLS_TENANT_COLUMN_MISMATCH", Detail: fmt.Sprintf("RLS evidence names tenant column %q, registry declares %q", binding.TenantColumn, wantColumn)})
		}
		if !binding.UsingTenant {
			findings = append(findings, Finding{Table: name, Code: "RLS_USING_SCOPE_MISSING", Detail: "tenant_isolation USING clause does not bind the declared tenant column to the session tenant"})
		}
		if !binding.WithCheckTenant {
			findings = append(findings, Finding{Table: name, Code: "RLS_WITH_CHECK_SCOPE_MISSING", Detail: "tenant_isolation WITH CHECK clause does not bind the declared tenant column to the session tenant"})
		}
		if !binding.SessionTenant {
			findings = append(findings, Finding{Table: name, Code: "RLS_SESSION_SCOPE_MISSING", Detail: "tenant_isolation does not use app.tenant_id session context in both directions"})
		}
	}

	for _, scope := range repositories {
		key := strings.ToLower(strings.TrimSpace(scope.Table))
		table, ok := tenantTables[key]
		if !ok {
			continue
		}
		wantColumn := strings.ToLower(strings.TrimSpace(*table.TenantScopingColumn))
		if scope.TenantColumn != "" && strings.ToLower(strings.TrimSpace(scope.TenantColumn)) != wantColumn {
			findings = append(findings, Finding{
				Table: table.Table, Package: scope.Package, File: scope.File,
				Function: scope.Function, Code: "REPOSITORY_TENANT_COLUMN_MISMATCH",
				Detail: fmt.Sprintf("repository evidence names tenant column %q, registry declares %q", scope.TenantColumn, wantColumn),
			})
		}
		if !scope.Scoped {
			findings = append(findings, Finding{
				Table: table.Table, Package: scope.Package, File: scope.File,
				Function: scope.Function, Code: "REPOSITORY_SCOPE_MISSING",
				Detail: "repository evidence does not carry the declared tenant boundary",
			})
		}
	}
	return sortFindings(findings)
}

// ScanRLS extracts RLS declarations for the supplied registered tables from
// all migration files. RLS commonly lives in the shared tenant-isolation
// migration rather than beside each CREATE TABLE, so this intentionally does
// not restrict the scan to a table's creation migration.
func ScanRLS(migrationsDir string, tables []tableinventory.Table) ([]RLSBinding, error) {
	bindings := make(map[string]RLSBinding)
	for _, table := range tables {
		if !table.TenantScoped() {
			continue
		}
		bindings[strings.ToLower(table.Table)] = RLSBinding{Table: table.Table, TenantColumn: strings.ToLower(*table.TenantScopingColumn)}
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		text := string(body)
		for _, match := range alterRLSPattern.FindAllStringSubmatch(text, -1) {
			name := strings.ToLower(match[1])
			binding, ok := bindings[name]
			if !ok {
				continue
			}
			switch strings.ToUpper(match[2]) {
			case "ENABLE":
				binding.Enabled = true
			case "FORCE":
				binding.Forced = true
			}
			bindings[name] = binding
		}
		for _, match := range createPolicyPattern.FindAllStringSubmatchIndex(text, -1) {
			name := strings.ToLower(text[match[2]:match[3]])
			binding, ok := bindings[name]
			if !ok {
				continue
			}
			end := strings.IndexByte(text[match[1]:], ';')
			if end < 0 {
				end = len(text) - match[1]
			}
			declaration := text[match[0] : match[1]+end]
			using, check := policyClauses(declaration, binding.TenantColumn)
			binding.Policy = true
			binding.PolicyMigration = entry.Name()
			binding.UsingTenant = binding.UsingTenant || using
			binding.WithCheckTenant = binding.WithCheckTenant || check
			binding.SessionTenant = binding.UsingTenant && binding.WithCheckTenant
			bindings[name] = binding
		}
		// Several migrations deliberately apply one RLS template to a
		// literal ARRAY of tables inside a PL/pgSQL loop. The table name is
		// therefore not present after %I at the source level. The template
		// and the quoted target list together are still sufficient bounded
		// evidence: only a table named in the same migration receives the
		// dynamic declaration.
		dynamicEnable := dynamicEnablePattern.MatchString(text)
		dynamicForce := dynamicForcePattern.MatchString(text)
		dynamicPolicy := dynamicPolicyPattern.MatchString(text)
		if dynamicEnable || dynamicForce || dynamicPolicy {
			for name, binding := range bindings {
				if !quotedTablePattern(name).MatchString(text) {
					continue
				}
				if dynamicEnable {
					binding.Enabled = true
				}
				if dynamicForce {
					binding.Forced = true
				}
				if dynamicPolicy {
					binding.Policy = true
					binding.PolicyMigration = entry.Name()
					binding.UsingTenant = true
					binding.WithCheckTenant = true
					binding.SessionTenant = true
				}
				bindings[name] = binding
			}
		}
	}

	out := make([]RLSBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, binding)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Table) < strings.ToLower(out[j].Table) })
	return out, nil
}

var alterRLSPattern = regexp.MustCompile(`(?im)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s+(ENABLE|FORCE|NO\s+FORCE)\s+ROW\s+LEVEL\s+SECURITY\b`)
var createPolicyPattern = regexp.MustCompile(`(?im)^\s*CREATE\s+POLICY\s+tenant_isolation\s+ON\s+(?:public\.)?([a-z_][a-z0-9_]*)\b`)
var dynamicEnablePattern = regexp.MustCompile(`(?is)EXECUTE\s+format\s*\(\s*'ALTER\s+TABLE\s+%I\s+ENABLE\s+ROW\s+LEVEL\s+SECURITY`)
var dynamicForcePattern = regexp.MustCompile(`(?is)EXECUTE\s+format\s*\(\s*'ALTER\s+TABLE\s+%I\s+FORCE\s+ROW\s+LEVEL\s+SECURITY`)
var dynamicPolicyPattern = regexp.MustCompile(`(?is)EXECUTE\s+format\s*\(\s*'CREATE\s+POLICY\s+tenant_isolation\s+ON\s+%I.*current_setting\s*\(\s*''app\.tenant_id''`)

func policyClauses(declaration, column string) (bool, bool) {
	upper := strings.ToUpper(declaration)
	usingAt := strings.Index(upper, "USING")
	checkAt := strings.Index(upper, "WITH CHECK")
	if usingAt < 0 || checkAt < 0 || checkAt <= usingAt {
		return false, false
	}
	using := declaration[usingAt:checkAt]
	check := declaration[checkAt:]
	return tenantClause(using, column), tenantClause(check, column)
}

func tenantClause(clause, column string) bool {
	return storeboundaries.WholeWordContains(strings.ToLower(clause), strings.ToLower(column)) && sessionTenantPattern.MatchString(clause)
}

var sessionTenantPattern = regexp.MustCompile(`(?i)current_setting\s*\(\s*['"]app\.tenant_id['"]`)

func quotedTablePattern(table string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)['"]` + regexp.QuoteMeta(table) + `['"]`)
}

func countReviewed(scopes []RepositoryScope) int {
	count := 0
	for _, scope := range scopes {
		if scope.Disposition == DispositionReviewedException {
			count++
		}
	}
	return count
}

func tenantColumn(tables []tableinventory.Table, table string) string {
	for _, entry := range tables {
		if strings.EqualFold(entry.Table, table) && entry.TenantScopingColumn != nil {
			return strings.ToLower(strings.TrimSpace(*entry.TenantScopingColumn))
		}
	}
	return ""
}

func sortFindings(findings []Finding) []Finding {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Table != findings[j].Table {
			return findings[i].Table < findings[j].Table
		}
		if findings[i].Package != findings[j].Package {
			return findings[i].Package < findings[j].Package
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Function != findings[j].Function {
			return findings[i].Function < findings[j].Function
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}
