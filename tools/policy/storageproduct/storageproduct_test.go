package storageproduct_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/productslice"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/storageproduct"
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

func alignmentFixture() tableinventory.AlignmentRegistry {
	return tableinventory.AlignmentRegistry{
		SchemaVersion: 1,
		Tables: []tableinventory.AlignmentTable{
			{Table: "zeta", Role: "PROJECTION", Owner: "internal/zstore", Migration: "00006_zeta.sql", TenantScoped: true},
			{Table: "alpha", Role: "LEDGER", Owner: "internal/astore", Migration: "00005_alpha.sql", TenantScoped: true},
		},
	}
}

func TestTodo_ALIGN_016(t *testing.T) {
	index, err := storageproduct.Evaluate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if !index.OK() || len(index.Entries) < 200 {
		t.Fatalf("checked-in reverse index = %+v", index)
	}
	if index.Explain() == "" || index.Digest() == "" {
		t.Fatal("reverse index has no stable explanation or digest")
	}
}

func TestTodo_ALIGN_016_Property(t *testing.T) {
	slices := []productslice.ProductSliceDefinition{{
		SliceID:  "promotion",
		Version:  1,
		Packages: []string{"github.com/monstercameron/human-capital-management-suite/internal/astore"},
	}}
	index := storageproduct.Generate(alignmentFixture(), slices, map[string][]string{
		"alpha": {"internal/consumer"},
		"zeta":  nil,
	})
	if len(index.Findings) != 0 || len(index.Entries) != 2 {
		t.Fatalf("reverse index findings/entries = %+v / %+v", index.Findings, index.Entries)
	}
	if index.Entries[0].Table != "alpha" || len(index.Entries[0].ProductSlices) != 1 || index.Entries[0].ProductSlices[0] != "promotion@1" {
		t.Fatalf("reverse index is not sorted or associated by owner: %+v", index.Entries)
	}
	if len(index.Entries[1].ProductSlices) != 0 {
		t.Fatalf("unadmitted storage was attributed to a slice: %+v", index.Entries[1])
	}
}

func TestTodo_ALIGN_016_Golden(t *testing.T) {
	index := storageproduct.Generate(alignmentFixture(), nil, nil)
	if len(index.Entries) != 2 || index.Entries[0].Table != "alpha" || index.Entries[1].Table != "zeta" {
		t.Fatalf("reverse index order = %+v", index.Entries)
	}
	if index.Digest() != index.Digest() {
		t.Fatal("reverse index digest is not stable")
	}
}

func TestTodo_ALIGN_016_Security(t *testing.T) {
	alignment := tableinventory.AlignmentRegistry{Tables: []tableinventory.AlignmentTable{{Table: "private", Owner: "internal/private"}}}
	index := storageproduct.Generate(alignment, []productslice.ProductSliceDefinition{{SliceID: "other", Version: 1, Packages: []string{"internal/not-private"}}}, map[string][]string{"private": {"internal/not-private"}})
	if len(index.Entries) != 1 || len(index.Entries[0].ProductSlices) != 1 {
		t.Fatalf("declared consumer association was lost: %+v", index)
	}
	spoofed := storageproduct.Generate(alignment, []productslice.ProductSliceDefinition{{SliceID: "other", Version: 1, Packages: []string{"internal/not-private"}}}, map[string][]string{"private": {"internal/not-private/child"}})
	if len(spoofed.Entries[0].ProductSlices) != 0 {
		t.Fatalf("subpackage consumer was incorrectly treated as a declared package: %+v", spoofed.Entries[0])
	}
}

func TestTodo_ALIGN_016_Conformance(t *testing.T) {
	if storageproduct.Version() != 1 {
		t.Fatalf("policy version = %d, want 1", storageproduct.Version())
	}
}
