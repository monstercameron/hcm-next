package productslice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRegistrySetsSchemaAndDigest(t *testing.T) {
	r := NewRegistry(validFixtureSlice())
	if r.Schema != RegistrySchema {
		t.Errorf("Schema = %q, want %q", r.Schema, RegistrySchema)
	}
	if r.SchemaVersion != RegistrySchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", r.SchemaVersion, RegistrySchemaVersion)
	}
	if r.Digest == "" {
		t.Error("Digest was not computed")
	}
	if err := r.VerifyDigest(); err != nil {
		t.Errorf("VerifyDigest() = %v, want nil for a freshly built registry", err)
	}
}

func TestRegistryVerifyDigestCatchesHandEditedDigest(t *testing.T) {
	r := NewRegistry(validFixtureSlice())
	r.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000"
	if err := r.VerifyDigest(); err == nil {
		t.Fatal("VerifyDigest() accepted a digest that does not match the content")
	}
}

func TestRegistryValidateRejectsDuplicateSliceID(t *testing.T) {
	slice := validFixtureSlice()
	r := NewRegistry(slice, slice)
	violations := r.Validate(fixtureRegistries())

	found := false
	for _, v := range violations {
		if v.Field == "slice_id" && v.Ref == slice.SliceID && v.Detail == "duplicate slice id in registry" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a duplicate slice_id violation, got: %v", ViolationStrings(violations))
	}
}

func TestRegistryValidatePassesThroughPerSliceViolations(t *testing.T) {
	broken := validFixtureSlice()
	broken.SliceID = "broken"
	broken.Capabilities = []string{"does-not-exist"}

	r := NewRegistry(broken)
	violations := r.Validate(fixtureRegistries())
	if Valid(violations) {
		t.Fatal("expected the broken slice's capability violation to propagate through Registry.Validate")
	}
	foundPrefixed := false
	for _, v := range violations {
		if v.Field == "capabilities" && v.Ref == "does-not-exist" && strings.HasPrefix(v.Detail, "slice broken: ") {
			foundPrefixed = true
		}
	}
	if !foundPrefixed {
		t.Fatalf("expected the violation detail to be prefixed with the slice id, got: %v", ViolationStrings(violations))
	}
}

func TestLoadRegistryYAMLRoundTrip(t *testing.T) {
	original := NewRegistry(validFixtureSlice())
	data, err := MarshalRegistryYAML(original)
	if err != nil {
		t.Fatalf("MarshalRegistryYAML: %v", err)
	}

	path := filepath.Join(t.TempDir(), "product-slices.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	loaded, err := LoadRegistryYAML(path)
	if err != nil {
		t.Fatalf("LoadRegistryYAML: %v", err)
	}
	if loaded.Digest != original.Digest {
		t.Errorf("round-tripped digest = %q, want %q", loaded.Digest, original.Digest)
	}
	if len(loaded.Slices) != 1 || loaded.Slices[0].SliceID != "promotion" {
		t.Fatalf("round-tripped slices = %+v, want one promotion slice", loaded.Slices)
	}
	if err := loaded.VerifyDigest(); err != nil {
		t.Errorf("round-tripped registry failed VerifyDigest: %v", err)
	}
}

func TestLoadRegistryYAMLMissingFile(t *testing.T) {
	_, err := LoadRegistryYAML(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestRenderRegistryFileIncludesHeader(t *testing.T) {
	data, err := RenderRegistryFile(NewRegistry(validFixtureSlice()))
	if err != nil {
		t.Fatalf("RenderRegistryFile: %v", err)
	}
	if !strings.HasPrefix(string(data), GeneratedFileHeader) {
		t.Fatal("rendered file does not start with GeneratedFileHeader")
	}
	if !strings.Contains(string(data), "DO NOT EDIT") {
		t.Fatal("rendered file header does not warn against hand-editing")
	}
}
