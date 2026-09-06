package tableownership_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/tableinventory"
	"github.com/monstercameron/hcm-next/tools/policy/tableownership"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func TestTodo_ALIGN_010(t *testing.T) {
	report, err := tableownership.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.Code == "OWNERLESS" || finding.Code == "OWNER_PACKAGE_MISSING" {
			t.Fatalf("checked-in registry has an owner violation: %+v", finding)
		}
	}
}

func TestTodo_ALIGN_010_Property(t *testing.T) {
	inventory := tableinventory.Inventory{Tables: []tableinventory.Table{{Table: "owned", OwnerPackage: "internal/example"}}}
	if findings := tableownership.Validate(inventory, map[string][]string{"owned": {"internal/example"}}); len(findings) != 0 {
		t.Fatalf("valid owner was rejected: %+v", findings)
	}
}

func TestTodo_ALIGN_010_Golden(t *testing.T) {
	inventory := tableinventory.Inventory{Tables: []tableinventory.Table{{Table: "ownerless"}}}
	findings := tableownership.Validate(inventory, map[string][]string{"ownerless": {"internal/example"}})
	if len(findings) != 1 || findings[0].Code != "OWNERLESS" {
		t.Fatalf("ownerless table findings = %+v", findings)
	}
}

func TestTodo_ALIGN_010_Security(t *testing.T) {
	inventory := tableinventory.Inventory{Tables: []tableinventory.Table{{Table: "owned", OwnerPackage: "internal/example"}}}
	if findings := tableownership.Validate(inventory, nil); len(findings) != 1 || findings[0].Code != "CONSUMERLESS" {
		t.Fatalf("missing consumer was not refused: %+v", findings)
	}
}

func TestTodo_ALIGN_010_Conformance(t *testing.T) {
	if tableownership.Version() != 1 {
		t.Fatalf("policy version = %d, want 1", tableownership.Version())
	}
}

func TestTodo_ALIGN_011(t *testing.T) {
	report, err := tableownership.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("consumer check passed without reporting the current unconsumed tables")
	}
	for _, finding := range report.Findings {
		if finding.Code == "CONSUMERLESS" {
			return
		}
	}
	t.Fatalf("current report has no consumerless finding: %+v", report.Findings)
}

func TestTodo_ALIGN_011_Property(t *testing.T) {
	inventory := tableinventory.Inventory{Tables: []tableinventory.Table{{Table: "consumed", OwnerPackage: "internal/example"}}}
	consumers := map[string][]string{"consumed": {"internal/example"}}
	if findings := tableownership.Validate(inventory, consumers); len(findings) != 0 {
		t.Fatalf("consumer was rejected: %+v", findings)
	}
}

func TestTodo_ALIGN_011_Golden(t *testing.T) {
	inventory := tableinventory.Inventory{Tables: []tableinventory.Table{
		{Table: "zeta", OwnerPackage: "internal/z"},
		{Table: "alpha", OwnerPackage: "internal/a"},
	}}
	findings := tableownership.Validate(inventory, map[string][]string{"alpha": {"internal/a"}})
	if len(findings) != 1 || findings[0].Table != "zeta" || findings[0].Code != "CONSUMERLESS" {
		t.Fatalf("consumerless golden findings = %+v", findings)
	}
}

func TestTodo_ALIGN_011_Security(t *testing.T) {
	inventory := tableinventory.Inventory{Tables: []tableinventory.Table{{Table: "private", OwnerPackage: "internal/example"}}}
	if findings := tableownership.Validate(inventory, map[string][]string{"private": nil}); len(findings) != 1 {
		t.Fatalf("a private table without a consumer passed: %+v", findings)
	}
}

func TestTodo_ALIGN_011_Conformance(t *testing.T) {
	report, err := tableownership.Evaluate(repoRoot(t))
	if err != nil || report.Consumers == nil {
		t.Fatalf("consumer evidence was not retained: report=%+v err=%v", report, err)
	}
}
