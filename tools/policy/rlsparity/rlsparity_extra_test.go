package rlsparity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/rlsparity"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory"
)

func TestRLSParity_MetadataAndReportMethods(t *testing.T) {
	if rlsparity.Version() != 1 || !strings.Contains(rlsparity.Explain(), "parity policy v1") {
		t.Fatalf("policy metadata is incomplete: version=%d explain=%q", rlsparity.Version(), rlsparity.Explain())
	}
	withLocation := rlsparity.Finding{Table: "worker", Package: "pkg", File: "store.go", Function: "List", Code: "MISSING", Detail: "bad"}
	if got := withLocation.Error(); !strings.Contains(got, "worker (pkg/store.go:List): MISSING: bad") {
		t.Fatalf("Finding.Error() = %q", got)
	}
	withoutLocation := rlsparity.Finding{Table: "worker", Code: "MISSING", Detail: "bad"}
	if got := withoutLocation.Error(); !strings.Contains(got, "worker: MISSING: bad") {
		t.Fatalf("Finding.Error() without package = %q", got)
	}
	clean := rlsparity.Report{RLS: []rlsparity.RLSBinding{{Table: "worker"}}, RepositoryScopes: []rlsparity.RepositoryScope{{Table: "worker"}}}
	if !clean.OK() || !strings.Contains(clean.Explain(), "1 RLS binding(s)") {
		t.Fatalf("clean report methods disagree: ok=%v explain=%q", clean.OK(), clean.Explain())
	}
	dirty := rlsparity.Report{Findings: []rlsparity.Finding{{Table: "worker"}}}
	if dirty.OK() {
		t.Fatal("report with findings was reported OK")
	}
}

func TestRLSParity_ValidateSecurityBranches(t *testing.T) {
	tests := []struct {
		name    string
		binding *rlsparity.RLSBinding
		repos   []rlsparity.RepositoryScope
		codes   []string
	}{
		{name: "missing RLS", codes: []string{"RLS_MISSING"}},
		{name: "disabled", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); v.Enabled = false; return &v }(), codes: []string{"RLS_DISABLED"}},
		{name: "not forced", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); v.Forced = false; return &v }(), codes: []string{"FORCE_RLS_MISSING"}},
		{name: "no policy", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); v.Policy = false; return &v }(), codes: []string{"TENANT_POLICY_MISSING"}},
		{name: "column mismatch", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); v.TenantColumn = "org_id"; return &v }(), codes: []string{"RLS_TENANT_COLUMN_MISMATCH"}},
		{name: "using missing", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); v.UsingTenant = false; return &v }(), codes: []string{"RLS_USING_SCOPE_MISSING"}},
		{name: "with check missing", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); v.WithCheckTenant = false; return &v }(), codes: []string{"RLS_WITH_CHECK_SCOPE_MISSING"}},
		{name: "session setting missing", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); v.SessionTenant = false; return &v }(), codes: []string{"RLS_SESSION_SCOPE_MISSING"}},
		{name: "repository column and scope", binding: func() *rlsparity.RLSBinding { v := goodRLS("worker"); return &v }(), repos: []rlsparity.RepositoryScope{{Table: "worker", Package: "pkg", TenantColumn: "org_id", Scoped: false}}, codes: []string{"REPOSITORY_SCOPE_MISSING", "REPOSITORY_TENANT_COLUMN_MISMATCH"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bindings []rlsparity.RLSBinding
			if tt.binding != nil {
				bindings = []rlsparity.RLSBinding{*tt.binding}
			}
			findings := rlsparity.Validate([]tableinventory.Table{tenantTable("worker"), {Table: "catalog"}}, bindings, tt.repos)
			got := make([]string, len(findings))
			for i, finding := range findings {
				got[i] = finding.Code
			}
			if len(got) != len(tt.codes) {
				t.Fatalf("finding codes = %v, want %v; findings=%+v", got, tt.codes, findings)
			}
			for i := range got {
				if got[i] != tt.codes[i] {
					t.Fatalf("finding codes = %v, want %v", got, tt.codes)
				}
			}
		})
	}
}

func TestRLSParity_ScanRLS_DirectAndDynamicDeclarations(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ALTER TABLE worker ENABLE ROW LEVEL SECURITY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "policy.sql"), []byte(`
ALTER TABLE worker ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON worker USING (tenant_id = current_setting('app.tenant_id')) WITH CHECK (tenant_id = current_setting('app.tenant_id'));
DO $$
BEGIN
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''app.tenant_id'')) WITH CHECK (tenant_id = current_setting(''app.tenant_id''))', table_name);
END $$;
-- 'job'
`), 0o600); err != nil {
		t.Fatal(err)
	}
	tables := []tableinventory.Table{tenantTable("worker"), tenantTable("job")}
	bindings, err := rlsparity.ScanRLS(dir, tables)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("ScanRLS returned %d bindings, want 2: %+v", len(bindings), bindings)
	}
	for _, binding := range bindings {
		if !binding.Enabled || !binding.Forced || !binding.Policy || !binding.UsingTenant || !binding.WithCheckTenant || !binding.SessionTenant || binding.PolicyMigration != "policy.sql" {
			t.Errorf("incomplete binding: %+v", binding)
		}
	}
	if _, err := rlsparity.ScanRLS(filepath.Join(dir, "missing"), tables); err == nil {
		t.Fatal("ScanRLS accepted a missing migrations directory")
	}
}

func TestRLSParity_CheckRejectsUnreviewedAndEvaluateRejectsMissingRoot(t *testing.T) {
	if err := rlsparity.Check(repoRoot(t)); err != nil {
		t.Fatalf("Check must pass once every repository-scope gap is reviewed in the storeboundaries allowlist, got %v", err)
	}
	if _, err := rlsparity.Evaluate(t.TempDir()); err == nil {
		t.Fatal("Evaluate accepted a root without the authoritative table registry")
	}
}
