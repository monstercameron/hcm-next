package rlsparity_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/rlsparity"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func tenantTable(name string) tableinventory.Table {
	column := "tenant_id"
	return tableinventory.Table{Table: name, TenantScopingColumn: &column}
}

func goodRLS(name string) rlsparity.RLSBinding {
	return rlsparity.RLSBinding{
		Table: name, TenantColumn: "tenant_id", PolicyMigration: "00008_tenant_isolation.sql",
		Enabled: true, Forced: true, Policy: true, UsingTenant: true,
		WithCheckTenant: true, SessionTenant: true,
	}
}

func TestTodo_ALIGN_013(t *testing.T) {
	report, err := rlsparity.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	// The workflow_timer repository-scope gap is reviewed in
	// tools/policy/storeboundaries/allowlist.yaml (the scheduler's cross-tenant
	// due-timer sweep), so parity reports it as a reviewed exception, never as
	// an unreviewed finding; any other finding is a parity break.
	for _, finding := range report.Findings {
		t.Fatalf("unreviewed row-security parity finding: %v", finding)
	}
	seenReviewed := false
	for _, scope := range report.RepositoryScopes {
		if scope.Table == "workflow_timer" && scope.Disposition == rlsparity.DispositionReviewedException {
			seenReviewed = true
		}
	}
	if !seenReviewed || report.ReviewedExceptions == 0 {
		t.Fatalf("documented workflow_timer exception was not retained as a reviewed exception: %+v", report.RepositoryScopes)
	}
	if len(report.RLS) == 0 || len(report.RepositoryScopes) == 0 || report.Explain() == "" {
		t.Fatalf("parity report lacks retained evidence: %+v", report)
	}
}

func TestTodo_ALIGN_013_Property(t *testing.T) {
	tables := []tableinventory.Table{tenantTable("worker")}
	if findings := rlsparity.Validate(tables, []rlsparity.RLSBinding{goodRLS("worker")}, []rlsparity.RepositoryScope{{Table: "worker", Package: "internal/data/workforce", Scoped: true, Disposition: rlsparity.DispositionVerified}}); len(findings) != 0 {
		t.Fatalf("valid RLS/repository parity was rejected: %+v", findings)
	}
}

func TestTodo_ALIGN_013_Golden(t *testing.T) {
	binding := goodRLS("worker")
	binding.Forced = false
	findings := rlsparity.Validate([]tableinventory.Table{tenantTable("worker")}, []rlsparity.RLSBinding{binding}, nil)
	if len(findings) != 1 || findings[0].Code != "FORCE_RLS_MISSING" {
		t.Fatalf("missing FORCE RLS finding = %+v", findings)
	}

	binding = goodRLS("worker")
	binding.TenantColumn = "organization_id"
	findings = rlsparity.Validate([]tableinventory.Table{tenantTable("worker")}, []rlsparity.RLSBinding{binding}, nil)
	if len(findings) != 1 || findings[0].Code != "RLS_TENANT_COLUMN_MISMATCH" {
		t.Fatalf("mismatched RLS tenant column finding = %+v", findings)
	}
}

func TestTodo_ALIGN_013_Security(t *testing.T) {
	findings := rlsparity.Validate(
		[]tableinventory.Table{tenantTable("worker")},
		[]rlsparity.RLSBinding{goodRLS("worker")},
		[]rlsparity.RepositoryScope{{Table: "worker", Package: "internal/data/workforce", File: "store.go", Function: "List", Scoped: false}},
	)
	if len(findings) != 1 || findings[0].Code != "REPOSITORY_SCOPE_MISSING" {
		t.Fatalf("unscoped repository read was accepted: %+v", findings)
	}
}

func TestTodo_ALIGN_013_Integration(t *testing.T) {
	report, err := rlsparity.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if report.ReviewedExceptions == 0 {
		t.Fatal("repository-scope review evidence was not retained")
	}
	for _, binding := range report.RLS {
		if !binding.Policy || !binding.Enabled || !binding.Forced || !binding.SessionTenant {
			t.Fatalf("incomplete RLS binding for %s: %+v", binding.Table, binding)
		}
	}
}

func TestTodo_ALIGN_013_Fault(t *testing.T) {
	binding := goodRLS("worker")
	binding.Policy = false
	findings := rlsparity.Validate([]tableinventory.Table{tenantTable("worker")}, []rlsparity.RLSBinding{binding}, nil)
	if len(findings) != 1 || findings[0].Code != "TENANT_POLICY_MISSING" {
		t.Fatalf("missing tenant policy was accepted: %+v", findings)
	}

	findings = rlsparity.Validate([]tableinventory.Table{tenantTable("worker")}, nil, nil)
	if len(findings) != 1 || findings[0].Code != "RLS_MISSING" {
		t.Fatalf("missing RLS evidence was accepted: %+v", findings)
	}
}

func TestTodo_ALIGN_013_Conformance(t *testing.T) {
	if rlsparity.Version() != 1 {
		t.Fatalf("policy version = %d, want 1", rlsparity.Version())
	}
	if rlsparity.Explain() == "" {
		t.Fatal("policy has no explanation")
	}
}
