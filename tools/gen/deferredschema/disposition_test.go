package deferredschema

import (
	"reflect"
	"testing"
)

func TestDispositionRowsShapeAndCount(t *testing.T) {
	domains := Domains()
	rows := DispositionRows(domains)
	if len(rows) != 20 {
		t.Fatalf("DispositionRows() = %d rows, want 20 (10 domains x 2 tables)", len(rows))
	}

	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.Table] {
			t.Fatalf("table %q registered twice in the preview", r.Table)
		}
		seen[r.Table] = true

		if r.TenantScopingColumn == nil || *r.TenantScopingColumn != "tenant_id" {
			t.Errorf("%s: tenant_scoping_column not tenant_id", r.Table)
		}
		if r.IsolationPackage != "internal/data/tenancy" {
			t.Errorf("%s: isolation_package = %q", r.Table, r.IsolationPackage)
		}
		if r.EncryptionClass != "PLATFORM_MANAGED" {
			t.Errorf("%s: encryption_class = %q", r.Table, r.EncryptionClass)
		}
		if r.RetentionClass != "OPERATIONAL" && r.RetentionClass != "PERMANENT" {
			t.Errorf("%s: retention_class = %q", r.Table, r.RetentionClass)
		}
		if r.RetentionClass == "PERMANENT" && !r.AppendOnly {
			t.Errorf("%s: PERMANENT but not append_only", r.Table)
		}
		if r.Disposition != "DRAFT" && r.Disposition != "CONFORMANCE" {
			t.Errorf("%s: disposition = %q", r.Table, r.Disposition)
		}
		if r.Migration == "" || r.OwnerPackage == "" || r.Plane == "" || r.DataRole == "" {
			t.Errorf("%s: missing a required field", r.Table)
		}
	}
}

func TestRenderAndLoadDispositionPreviewYAMLRoundTrips(t *testing.T) {
	domains := Domains()
	yamlText, err := RenderDispositionPreviewYAML(domains)
	if err != nil {
		t.Fatalf("RenderDispositionPreviewYAML: %v", err)
	}
	rows, err := LoadDispositionPreviewYAML([]byte(yamlText))
	if err != nil {
		t.Fatalf("LoadDispositionPreviewYAML: %v", err)
	}
	want := DispositionRows(domains)
	if len(rows) != len(want) {
		t.Fatalf("round-tripped %d rows, want %d", len(rows), len(want))
	}
	for i := range want {
		got, wantRow := rows[i], want[i]
		// YAML round-trips a nil slice as an empty, non-nil one; normalize
		// before comparing since the two are equivalent for this row shape.
		if len(got.Partitions) == 0 {
			got.Partitions = nil
		}
		if len(wantRow.Partitions) == 0 {
			wantRow.Partitions = nil
		}
		if !reflect.DeepEqual(got, wantRow) {
			t.Fatalf("row %d round-tripped as %+v, want %+v", i, got, wantRow)
		}
	}
}

func TestRenderDispositionPreviewYAMLDeterministic(t *testing.T) {
	domains := Domains()
	a, err := RenderDispositionPreviewYAML(domains)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderDispositionPreviewYAML(domains)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("RenderDispositionPreviewYAML is not deterministic across calls")
	}
}
