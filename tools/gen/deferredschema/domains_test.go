package deferredschema

import (
	"path/filepath"
	"runtime"
	"testing"
)

// testRepoRoot locates the repository root from this test file's own
// location. It duplicates tools/policy/internal/repopath.RootDir's approach
// rather than importing it: that package is internal to tools/policy, so
// tools/gen/deferredschema cannot import it.
func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// tools/gen/deferredschema/domains_test.go -> repo root is three levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

var expectedDomainOrder = []string{
	"payroll", "benefits", "time", "leave", "recruiting",
	"talent", "learning", "case", "access", "regulatory",
}

// TestTodo_DB_016 is the primary test: the ten future-domain entities named
// in DB-016's GREEN clause each produce a Head/Evidence table pair with a
// validated DRAFT/CONFORMANCE disposition, and none of the twenty resulting
// table names already exist as production authority (migrations/ or the
// storage-disposition registry), and the Phase 1 capability registry grants
// none of them write access.
func TestTodo_DB_016(t *testing.T) {
	domains := Domains()
	if len(domains) != 10 {
		t.Fatalf("Domains() returned %d domains, want 10", len(domains))
	}
	for i, d := range domains {
		if d.Slug != expectedDomainOrder[i] {
			t.Fatalf("domain[%d] = %q, want %q (GREEN clause order)", i, d.Slug, expectedDomainOrder[i])
		}
		if d.Number != i+1 {
			t.Fatalf("domain %q Number = %d, want %d", d.Slug, d.Number, i+1)
		}
		if d.Disposition != "DRAFT" && d.Disposition != "CONFORMANCE" {
			t.Fatalf("domain %q disposition = %q, want DRAFT or CONFORMANCE", d.Slug, d.Disposition)
		}
		if d.Head.Table == "" || d.Evidence.Table == "" {
			t.Fatalf("domain %q missing a Head or Evidence table name", d.Slug)
		}
		if !d.Evidence.AppendOnly {
			t.Fatalf("domain %q Evidence table %q must be AppendOnly", d.Slug, d.Evidence.Table)
		}
		if d.Head.AppendOnly {
			t.Fatalf("domain %q Head table %q must not be AppendOnly", d.Slug, d.Head.Table)
		}
		if d.Head.RetentionClass != "OPERATIONAL" {
			t.Fatalf("domain %q Head retention_class = %q, want OPERATIONAL", d.Slug, d.Head.RetentionClass)
		}
		if d.Evidence.RetentionClass != "PERMANENT" {
			t.Fatalf("domain %q Evidence retention_class = %q, want PERMANENT", d.Slug, d.Evidence.RetentionClass)
		}
	}

	// Exactly Leave is CONFORMANCE (DB-023 is the only funded
	// materialize-domain todo today); every other domain is DRAFT.
	for _, d := range domains {
		want := "DRAFT"
		if d.Slug == "leave" {
			want = "CONFORMANCE"
		}
		if d.Disposition != want {
			t.Errorf("domain %q disposition = %q, want %q", d.Slug, d.Disposition, want)
		}
	}

	set, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	root := testRepoRoot(t)
	report, err := Validate(root, domains, set)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := report.Error(); err != nil {
		t.Fatalf("GREEN violated: %v", err)
	}
}
