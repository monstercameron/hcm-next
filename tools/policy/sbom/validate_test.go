package sbom_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
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

func TestValidateCompletenessReportsReadErrorsAndRendersAllFindings(t *testing.T) {
	if _, err := sbom.ValidateCompleteness(&sbom.Document{}, t.TempDir()); err == nil {
		t.Fatal("ValidateCompleteness accepted a root without go.mod")
	}
	if _, err := sbom.ValidateCompletenessAt(&sbom.Document{}, t.TempDir(), time.Now()); err == nil {
		t.Fatal("ValidateCompletenessAt accepted a root without go.mod")
	}

	result := sbom.Completeness{
		RootMismatch:          "root mismatch",
		MissingRequires:       []string{"example.com/m@v1"},
		VersionlessRefs:       []string{"root (x)"},
		MissingLicenses:       []string{"example.com/m@v1"},
		InvalidLicenseRecords: []string{"example.com/m@v1 expired"},
	}
	if result.Empty() {
		t.Fatal("non-empty completeness result reported Empty")
	}
	errors := result.Errors()
	if len(errors) != 5 || errors[0] != "root mismatch" {
		t.Fatalf("Completeness.Errors = %v, want all five rendered findings", errors)
	}
	if !((sbom.Completeness{}).Empty()) {
		t.Fatal("zero completeness result is not Empty")
	}
}

func TestValidateLicenseCompletenessRejectsNilAndMalformedExpiry(t *testing.T) {
	nilResult := sbom.ValidateLicenseCompletenessAt(nil, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	if len(nilResult.InvalidLicenseRecords) != 1 || nilResult.InvalidLicenseRecords[0] != "document is nil" {
		t.Fatalf("nil document result = %+v", nilResult)
	}
	doc := &sbom.Document{
		Components: []sbom.Component{{Name: "example.com/m", Version: "v1", License: sbom.UnknownLicense}},
		LicenseExceptions: []sbom.LicenseException{
			{Component: "example.com/m", Version: "v1", Reason: "reason", Reviewer: "reviewer", Expiry: "not-a-date"},
			{Component: "example.com/other", Version: "v1", Reason: "reason", Reviewer: "reviewer", Expiry: "2027-01-01"},
		},
	}
	result := sbom.ValidateLicenseCompletenessAt(doc, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	if len(result.InvalidLicenseRecords) != 1 || len(result.MissingLicenses) != 1 {
		t.Fatalf("malformed/misapplied exceptions = %+v, want one invalid and one missing", result)
	}
}
