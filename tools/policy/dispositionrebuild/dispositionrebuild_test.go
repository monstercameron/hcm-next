package dispositionrebuild_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/dispositionrebuild"
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

func validTables() []tableinventory.Table {
	source := "ledger_event"
	return []tableinventory.Table{
		{Table: "ledger_event", DataRole: storagedisposition.RoleLedger},
		{Table: "projection", DataRole: storagedisposition.RoleProjection, RetentionClass: storagedisposition.RetentionRebuildable, RebuildSource: &source},
	}
}

func validMigrationTables() []tableinventory.MigrationTable {
	return []tableinventory.MigrationTable{{Name: "ledger_event", Migration: "00005_ledger.sql"}, {Name: "projection", Migration: "00006_projection.sql"}}
}

func TestTodo_ALIGN_015(t *testing.T) {
	report, err := dispositionrebuild.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || len(report.Plans) != 4 {
		t.Fatalf("checked-in rebuild disposition report = %+v", report)
	}
}

func TestTodo_ALIGN_015_Property(t *testing.T) {
	findings, plans := dispositionrebuild.Validate(validTables(), validMigrationTables())
	if len(findings) != 0 || len(plans) != 1 || plans[0].Source != "ledger_event" {
		t.Fatalf("valid rebuild source result = findings=%+v plans=%+v", findings, plans)
	}
}

func TestTodo_ALIGN_015_Golden(t *testing.T) {
	findings, _ := dispositionrebuild.Validate(validTables(), []tableinventory.MigrationTable{{Name: "projection"}})
	if len(findings) != 1 || findings[0].Code != "REBUILD_SOURCE_NOT_MIGRATED" {
		t.Fatalf("missing physical source finding = %+v", findings)
	}
}

func TestTodo_ALIGN_015_Security(t *testing.T) {
	source := "projection"
	tables := []tableinventory.Table{
		{Table: "projection", DataRole: storagedisposition.RoleProjection, RetentionClass: storagedisposition.RetentionRebuildable, RebuildSource: &source},
	}
	findings, _ := dispositionrebuild.Validate(tables, []tableinventory.MigrationTable{{Name: "projection"}})
	if len(findings) != 1 || findings[0].Code != "REBUILD_SOURCE_SELF" {
		t.Fatalf("self-rebuild source was accepted: %+v", findings)
	}
}

func TestTodo_ALIGN_015_Conformance(t *testing.T) {
	if dispositionrebuild.Version() != 1 {
		t.Fatalf("policy version = %d, want 1", dispositionrebuild.Version())
	}
}
