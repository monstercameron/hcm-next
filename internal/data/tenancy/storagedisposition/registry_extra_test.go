package storagedisposition_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

func TestTableEntryTenantScoped_Boundaries(t *testing.T) {
	var nilColumn *string
	empty := ""
	column := "tenant_id"
	for name, entry := range map[string]storagedisposition.TableEntry{
		"nil column":   {TenantScopingColumn: nilColumn},
		"empty column": {TenantScopingColumn: &empty},
		"named column": {TenantScopingColumn: &column},
	} {
		t.Run(name, func(t *testing.T) {
			want := name == "named column"
			if got := entry.TenantScoped(); got != want {
				t.Fatalf("TenantScoped() = %v, want %v", got, want)
			}
		})
	}
}

func TestRegistryLookupAndNames_SortAndMiss(t *testing.T) {
	r := &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{
		{Table: "zeta"}, {Table: "alpha"}, {Table: "middle"},
	}}
	if got := r.TableNames(); !slices.Equal(got, []string{"alpha", "middle", "zeta"}) {
		t.Fatalf("TableNames() = %v", got)
	}
	if got, ok := r.Lookup("middle"); !ok || got.Table != "middle" {
		t.Fatalf("Lookup(middle) = %+v, %v", got, ok)
	}
	if got, ok := r.Lookup("missing"); ok || got.Table != "" || got.Migration != "" {
		t.Fatalf("Lookup(missing) = %+v, %v; want zero, false", got, ok)
	}
}

func TestLoad_ReportsReadAndParseErrors(t *testing.T) {
	if _, err := storagedisposition.Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("Load accepted a missing registry")
	}
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(path, []byte("tables: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := storagedisposition.Load(path); err == nil {
		t.Fatal("Load accepted malformed YAML")
	}
}

func TestValidate_RejectsEveryStructuralBoundary(t *testing.T) {
	ledger := storagedisposition.TableEntry{Table: "ledger", Migration: "001.sql", OwnerPackage: "pkg", Plane: "DATA", DataRole: storagedisposition.RoleLedger, RetentionClass: storagedisposition.RetentionPermanent, EncryptionClass: storagedisposition.EncryptionPlatformManaged}
	base := func() *storagedisposition.Registry {
		return &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{ledger}}
	}
	tests := []struct {
		name   string
		mutate func(*storagedisposition.Registry)
	}{
		{"empty table", func(r *storagedisposition.Registry) { r.Tables[0].Table = "" }},
		{"duplicate table", func(r *storagedisposition.Registry) { r.Tables = append(r.Tables, r.Tables[0]) }},
		{"missing migration", func(r *storagedisposition.Registry) { r.Tables[0].Migration = "" }},
		{"missing owner", func(r *storagedisposition.Registry) { r.Tables[0].OwnerPackage = "" }},
		{"missing plane", func(r *storagedisposition.Registry) { r.Tables[0].Plane = "" }},
		{"invalid role", func(r *storagedisposition.Registry) { r.Tables[0].DataRole = "INVALID" }},
		{"invalid retention", func(r *storagedisposition.Registry) { r.Tables[0].RetentionClass = "INVALID" }},
		{"invalid encryption", func(r *storagedisposition.Registry) { r.Tables[0].EncryptionClass = "INVALID" }},
		{"scoped without isolation package", func(r *storagedisposition.Registry) { c := "tenant_id"; r.Tables[0].TenantScopingColumn = &c }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.mutate(r)
			if err := storagedisposition.Validate(r); err == nil {
				t.Fatal("Validate accepted malformed registry")
			}
		})
	}
}

func TestValidate_RebuildSourceBranches(t *testing.T) {
	ledger := storagedisposition.TableEntry{Table: "ledger", Migration: "001.sql", OwnerPackage: "pkg", Plane: "DATA", DataRole: storagedisposition.RoleLedger, RetentionClass: storagedisposition.RetentionPermanent, EncryptionClass: storagedisposition.EncryptionPlatformManaged}
	makeRegistry := func(source *string) *storagedisposition.Registry {
		return &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{ledger, {
			Table: "projection", Migration: "002.sql", OwnerPackage: "pkg", Plane: "DATA", DataRole: storagedisposition.RoleProjection,
			RetentionClass: storagedisposition.RetentionRebuildable, EncryptionClass: storagedisposition.EncryptionPlatformManaged, RebuildSource: source,
		}}}
	}
	missing := makeRegistry(nil)
	if err := storagedisposition.Validate(missing); err == nil {
		t.Fatal("REBUILDABLE entry without source passed validation")
	}
	unknown := "missing"
	if err := storagedisposition.Validate(makeRegistry(&unknown)); err == nil {
		t.Fatal("unknown rebuild source passed validation")
	}
	self := "projection"
	if err := storagedisposition.Validate(makeRegistry(&self)); err == nil {
		t.Fatal("self rebuild source passed validation")
	}
	nonLedger := "projection"
	reg := makeRegistry(&nonLedger)
	reg.Tables[0].DataRole = storagedisposition.RoleProjection
	if err := storagedisposition.Validate(reg); err == nil {
		t.Fatal("non-LEDGER rebuild source passed validation")
	}
	good := "ledger"
	if err := storagedisposition.Validate(makeRegistry(&good)); err != nil {
		t.Fatalf("valid rebuild source rejected: %v", err)
	}
}

func TestDiff_EmptyAndSortedDifferences(t *testing.T) {
	r := &storagedisposition.Registry{Tables: []storagedisposition.TableEntry{{Table: "b"}, {Table: "a"}}}
	if diff := r.CompareToLiveSchema([]string{"a", "b"}); !diff.Empty() {
		t.Fatalf("matching schema diff = %+v", diff)
	}
	diff := r.CompareToLiveSchema([]string{"c", "a", "a"})
	if diff.Empty() || !slices.Equal(diff.UnregisteredInLive, []string{"c"}) || !slices.Equal(diff.StaleInRegistry, []string{"b"}) {
		t.Fatalf("unexpected diff = %+v", diff)
	}
}
