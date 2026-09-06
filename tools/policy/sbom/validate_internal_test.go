package sbom

import (
	"testing"
	"time"
)

func TestValidateCompletenessAtMergesStructuralAndLicenseFindings(t *testing.T) {
	doc := &Document{
		Metadata:   Metadata{Component: Component{Name: "wrong-root", Version: ""}},
		Components: []Component{{Name: "example.com/m", Version: "", License: UnknownLicense}},
	}
	got := validateCompletenessAt(doc, []Require{{Path: "example.com/m", Version: "v1.0.0"}}, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	if got.RootMismatch == "" || len(got.VersionlessRefs) != 2 || len(got.MissingRequires) != 1 || len(got.MissingLicenses) != 1 {
		t.Fatalf("merged completeness = %+v, want structural and license findings", got)
	}
}

func TestMergeCompletenessAppendsOnlyLicenseFindings(t *testing.T) {
	left := Completeness{RootMismatch: "root", MissingRequires: []string{"a@v1"}}
	right := Completeness{MissingLicenses: []string{"b@v1"}, InvalidLicenseRecords: []string{"c@v1"}}
	got := mergeCompleteness(left, right)
	if got.RootMismatch != "root" || len(got.MissingRequires) != 1 || len(got.MissingLicenses) != 1 || len(got.InvalidLicenseRecords) != 1 {
		t.Fatalf("mergeCompleteness = %+v, want all findings retained", got)
	}
}
