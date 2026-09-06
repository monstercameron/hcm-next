package placementbindings_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/placementbindings"
	"github.com/monstercameron/hcm-next/tools/policy/tableinventory"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func validSchemas() []placementbindings.TableSchema {
	return []placementbindings.TableSchema{
		{Table: "tenant_data", Columns: map[string]string{"tenant_id": "tenant_ref NOT NULL"}, Policies: map[string]bool{"tenant_isolation": true}, ForceRLS: true},
		{Table: "tenant_placement", Columns: map[string]string{
			"tenant_id": "tenant_ref NOT NULL", "cell": "text NOT NULL", "epoch": "bigint NOT NULL", "signature": "bytea NOT NULL",
		}, Policies: map[string]bool{"tenant_isolation": true}, ForceRLS: true},
	}
}

func TestTodo_ALIGN_012(t *testing.T) {
	report, err := placementbindings.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		t.Log(finding.Error())
	}
	if len(report.Schemas) == 0 || report.Explain() == "" {
		t.Fatalf("placement report has no migration evidence: %+v", report)
	}
}

func TestTodo_ALIGN_012_Property(t *testing.T) {
	tables := []tableinventory.Table{{Table: "tenant_data", TenantScopingColumn: stringPtr("tenant_id")}}
	if findings := placementbindings.Validate(tables, validSchemas()); len(findings) != 0 {
		t.Fatalf("valid tenant binding was rejected: %+v", findings)
	}
}

func TestTodo_ALIGN_012_Golden(t *testing.T) {
	tables := []tableinventory.Table{{Table: "tenant_data", TenantScopingColumn: stringPtr("tenant_id")}}
	schemas := validSchemas()
	schemas[0].ForceRLS = false
	findings := placementbindings.Validate(tables, schemas)
	if len(findings) != 1 || findings[0].Code != "FORCE_RLS_MISSING" {
		t.Fatalf("placement golden findings = %+v", findings)
	}
}

func TestTodo_ALIGN_012_Security(t *testing.T) {
	tables := []tableinventory.Table{{Table: "tenant_data", TenantScopingColumn: stringPtr("tenant_id")}}
	schemas := validSchemas()
	schemas[0].Policies = nil
	findings := placementbindings.Validate(tables, schemas)
	if len(findings) != 1 || findings[0].Code != "TENANT_POLICY_MISSING" {
		t.Fatalf("missing tenant isolation policy was not refused: %+v", findings)
	}
}

func TestTodo_ALIGN_012_Conformance(t *testing.T) {
	if placementbindings.Version() != 1 {
		t.Fatalf("policy version = %d, want 1", placementbindings.Version())
	}
}

func TestTodo_ALIGN_014(t *testing.T) {
	report, err := placementbindings.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("default semantics findings: %d", len(report.Findings))
	if report.Schemas[0].Table == "" {
		t.Fatal("default-semantics check did not retain schema evidence")
	}
}

func TestTodo_ALIGN_014_Property(t *testing.T) {
	findings := placementbindings.ValidateDefaults([]placementbindings.TableSchema{{
		Table: "domain", Columns: map[string]string{
			"status": "text NOT NULL DEFAULT 'ACTIVE'", "created_at": "timestamptz NOT NULL DEFAULT now()",
		},
	}})
	if len(findings) != 1 || findings[0].Column != "status" || findings[0].Code != "DOMAIN_DEFAULT_UNAUTHORIZED" {
		t.Fatalf("default property findings = %+v", findings)
	}
}

func TestTodo_ALIGN_014_Golden(t *testing.T) {
	findings := placementbindings.ValidateDefaults([]placementbindings.TableSchema{{
		Table: "domain", Columns: map[string]string{"updated_at": "timestamptz NOT NULL DEFAULT now()"},
	}})
	if len(findings) != 0 {
		t.Fatalf("technical timestamp default was rejected: %+v", findings)
	}
}

func TestTodo_ALIGN_014_Security(t *testing.T) {
	findings := placementbindings.ValidateDefaults([]placementbindings.TableSchema{{
		Table: "domain", Columns: map[string]string{"state": "text NOT NULL DEFAULT 'OPEN'"},
	}})
	if len(findings) != 1 {
		t.Fatal("domain state default was accepted")
	}
}

func TestTodo_ALIGN_014_Conformance(t *testing.T) {
	if placementbindings.Version() != 1 {
		t.Fatalf("policy version = %d, want 1", placementbindings.Version())
	}
}

func stringPtr(value string) *string { return &value }
