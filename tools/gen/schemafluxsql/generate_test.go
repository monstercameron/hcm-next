package schemafluxsql

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest"
)

func TestGenerateIsDeterministicAndProducesReviewInputs(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	inv := storagemanifest.MigrationInventory{Tables: map[string]bool{}, StreamKinds: map[string]bool{}, ColumnChecks: map[string]map[string]bool{}, SourceFiles: []string{"00001.sql"}}
	a, err := Generate(reg, inv)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(reg, inv)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest() != b.Digest() {
		t.Fatalf("digest changed: %s != %s", a.Digest(), b.Digest())
	}
	if len(a.Inputs) == 0 {
		t.Fatal("expected missing dispositions to become review inputs")
	}
	for _, in := range a.Inputs {
		if in.SourceRef == "" || in.SQL == "" || in.Reason == "" {
			t.Fatalf("incomplete input: %+v", in)
		}
	}
}

func TestGenerateRejectsNilRegistry(t *testing.T) {
	_, err := Generate(nil, storagemanifest.MigrationInventory{})
	if err == nil {
		t.Fatal("expected nil registry error")
	}
}
