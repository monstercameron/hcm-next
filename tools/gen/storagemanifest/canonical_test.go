package storagemanifest

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/tools/gen/schemaflux/sources"
)

func TestCanonicalPropertyMappingStorageAndLineageClosureRejectsSemanticDrift(t *testing.T) {
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	inv, err := ScanMigrations("../../../migrations")
	if err != nil {
		t.Fatalf("migrations: %v", err)
	}
	props, err := BuildPropertyMappings(reg)
	if err != nil {
		t.Fatalf("properties: %v", err)
	}
	disp, err := BuildDispositionManifest(reg, inv)
	if err != nil {
		t.Fatalf("disposition: %v", err)
	}
	if got := CanonicalPropertyClosure(reg, props, disp); len(got) != 0 {
		t.Fatalf("canonical closure rejected clean sources: %v", got)
	}
	meta, err := sources.LoadMetamodel("../../../schema/schemaflux/metamodel/v1/metamodel.yaml")
	if err != nil {
		t.Fatalf("SchemaFlux metamodel: %v", err)
	}
	auth, retention, err := sources.LoadRegistries("../../../schema/schemaflux/registries/v1/registries.yaml")
	if err != nil {
		t.Fatalf("SchemaFlux registries: %v", err)
	}
	entities, relationships, err := sources.LoadEntityFamilies("../../../schema/schemaflux/entities/v1")
	if err != nil {
		t.Fatalf("SchemaFlux entities: %v", err)
	}
	compiled, sourceErrs := sources.Compile(sources.Bundle{Metamodel: meta, Authorities: auth, Retentions: retention, Entities: entities, Relationships: relationships})
	if len(sourceErrs) != 0 {
		t.Fatalf("SchemaFlux source compilation: %v", sourceErrs)
	}
	if mismatches, err := sources.CrossCheckModel(compiled); err != nil {
		t.Fatalf("SchemaFlux↔Go cross-check: %v", err)
	} else if len(mismatches) != 0 {
		t.Fatalf("SchemaFlux↔Go semantic drift: %v", mismatches)
	}

	props.Properties[0].Columns[0].SQLType = "jsonb"
	got := CanonicalPropertyClosure(reg, props, disp)
	if len(got) == 0 {
		t.Fatal("semantic SQL drift was accepted")
	}
	if got[0] == nil {
		t.Fatal("closure returned a nil diagnostic")
	}
}
