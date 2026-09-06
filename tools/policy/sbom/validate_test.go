package sbom_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/sbom"
)

func completeFixture() (*sbom.Document, []sbom.Require) {
	requires := []sbom.Require{
		{Path: "example.com/direct", Version: "v1.0.0"},
		{Path: "example.com/indirect", Version: "v2.0.0", Indirect: true},
	}
	doc := &sbom.Document{
		Metadata: sbom.Metadata{
			Component: sbom.Component{Name: sbom.RootModulePath, Version: "v0.0.0-devel"},
		},
		Components: []sbom.Component{
			{Name: "example.com/direct", Version: "v1.0.0"},
			{Name: "example.com/indirect", Version: "v2.0.0"},
		},
	}
	return doc, requires
}

// TestValidateCompletenessAgainst_Complete is the GREEN case: every go.mod
// require has a matching component, the root component matches, and every
// component has a version.
func TestValidateCompletenessAgainst_Complete(t *testing.T) {
	doc, requires := completeFixture()
	got := sbom.ValidateCompletenessAgainst(doc, requires)
	if !got.Empty() {
		t.Fatalf("ValidateCompletenessAgainst: expected complete, got %+v (%v)", got, got.Errors())
	}
}

// TestValidateCompletenessAgainst_MissingRequire is TOOL-017's named RED
// clause: an artifact (here, a BOM) missing a required module/version must
// be rejected.
func TestValidateCompletenessAgainst_MissingRequire(t *testing.T) {
	doc, requires := completeFixture()
	doc.Components = doc.Components[:1] // drop example.com/indirect
	got := sbom.ValidateCompletenessAgainst(doc, requires)
	if got.Empty() {
		t.Fatal("ValidateCompletenessAgainst: expected a missing-require finding, got Empty")
	}
	if len(got.MissingRequires) != 1 || got.MissingRequires[0] != "example.com/indirect@v2.0.0" {
		t.Errorf("MissingRequires = %v, want [example.com/indirect@v2.0.0]", got.MissingRequires)
	}
}

// TestValidateCompletenessAgainst_VersionMismatch proves a component present
// at the wrong version does not silently satisfy the require.
func TestValidateCompletenessAgainst_VersionMismatch(t *testing.T) {
	doc, requires := completeFixture()
	doc.Components[0].Version = "v9.9.9"
	got := sbom.ValidateCompletenessAgainst(doc, requires)
	if got.Empty() {
		t.Fatal("ValidateCompletenessAgainst: expected a version-mismatch finding, got Empty")
	}
}

// TestValidateCompletenessAgainst_VersionlessComponent is TOOL-017's other
// named RED clause: a component missing a version must be rejected.
func TestValidateCompletenessAgainst_VersionlessComponent(t *testing.T) {
	doc, requires := completeFixture()
	doc.Components[1].Version = ""
	got := sbom.ValidateCompletenessAgainst(doc, requires)
	if got.Empty() {
		t.Fatal("ValidateCompletenessAgainst: expected a versionless-component finding, got Empty")
	}
	found := false
	for _, v := range got.VersionlessRefs {
		if v == "example.com/indirect" {
			found = true
		}
	}
	if !found {
		t.Errorf("VersionlessRefs = %v, want to contain example.com/indirect", got.VersionlessRefs)
	}
}

// TestValidateCompletenessAgainst_RootMismatch is TOOL-017's third RED
// clause applied to the root/application component itself.
func TestValidateCompletenessAgainst_RootMismatch(t *testing.T) {
	doc, requires := completeFixture()
	doc.Metadata.Component.Name = "example.com/not-the-root"
	got := sbom.ValidateCompletenessAgainst(doc, requires)
	if got.RootMismatch == "" {
		t.Fatal("ValidateCompletenessAgainst: expected a root-mismatch finding, got none")
	}
}

// TestValidateCompletenessAgainst_RootVersionless folds the root component
// into the "no component lacks a version" rule.
func TestValidateCompletenessAgainst_RootVersionless(t *testing.T) {
	doc, requires := completeFixture()
	doc.Metadata.Component.Version = ""
	got := sbom.ValidateCompletenessAgainst(doc, requires)
	if got.Empty() {
		t.Fatal("ValidateCompletenessAgainst: expected a versionless-root finding, got Empty")
	}
}
