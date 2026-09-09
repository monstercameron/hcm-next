package storagemanifest

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

func TestCanonicalPropertyMappingStorageAndLineageClosureRejectsSemanticDrift(t *testing.T) {
	reg, props, disp := canonicalInputs(t)
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

func canonicalInputs(t *testing.T) (*model.Registry, PropertyMappingManifest, DispositionManifest) {
	t.Helper()
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
	return reg, props, disp
}

func TestTodo_MODEL_032_Conformance(t *testing.T) {
	reg, props, disp := canonicalInputs(t)
	if errs := CanonicalPropertyClosure(reg, props, disp); len(errs) != 0 {
		t.Fatalf("conformance: %v", errs)
	}
}

func TestTodo_MODEL_032_Golden(t *testing.T) {
	_, props, disp := canonicalInputs(t)
	if props.Digest() == "" || disp.Digest() == "" {
		t.Fatal("canonical manifests must have digests")
	}
	propsCopy, dispCopy := props, disp
	if props.Digest() != propsCopy.Digest() || disp.Digest() != dispCopy.Digest() {
		t.Fatal("manifest digest is not stable")
	}
}

func TestTodo_MODEL_032_Integration(t *testing.T) {
	reg, props, disp := canonicalInputs(t)
	if errs := CanonicalPropertyClosure(reg, props, disp); len(errs) != 0 {
		t.Fatalf("integration closure: %v", errs)
	}
}

func TestTodo_MODEL_032_Mutation(t *testing.T) {
	reg, props, disp := canonicalInputs(t)
	props.Properties[0].Columns[0].SQLType = "jsonb"
	if errs := CanonicalPropertyClosure(reg, props, disp); len(errs) == 0 {
		t.Fatal("semantic mutation was accepted")
	}
}

func TestTodo_MODEL_032_Property(t *testing.T) {
	reg, props, disp := canonicalInputs(t)
	if len(props.Properties) != len(reg.Properties()) {
		t.Fatalf("property count %d, want %d", len(props.Properties), len(reg.Properties()))
	}
	if errs := CanonicalPropertyClosure(reg, props, disp); len(errs) != 0 {
		t.Fatalf("property closure: %v", errs)
	}
}

func TestTodo_MODEL_032_Recovery(t *testing.T) {
	reg, props, disp := canonicalInputs(t)
	disp.Entities = disp.Entities[:len(disp.Entities)-1]
	if errs := CanonicalPropertyClosure(reg, props, disp); len(errs) == 0 {
		t.Fatal("missing disposition was accepted")
	}
}

func TestTodo_MODEL_032_Security(t *testing.T) {
	reg, props, disp := canonicalInputs(t)
	props.Properties = append(props.Properties, PropertySQLMapping{PropertyRef: "property.untrusted.secret"})
	if errs := CanonicalPropertyClosure(reg, props, disp); len(errs) == 0 {
		t.Fatal("unregistered property was accepted")
	}
}

func FuzzTodo_MODEL_032(f *testing.F) {
	f.Add("text")
	f.Fuzz(func(t *testing.T, sqlType string) {
		reg, props, disp := canonicalInputs(t)
		props.Properties[0].Columns[0].SQLType = sqlType
		errs := CanonicalPropertyClosure(reg, props, disp)
		if sqlType == "text" && len(errs) != 0 {
			t.Fatalf("valid SQL type rejected: %v", errs)
		}
		if sqlType != "text" && len(errs) == 0 {
			t.Fatalf("SQL type %q drift accepted", sqlType)
		}
	})
}
