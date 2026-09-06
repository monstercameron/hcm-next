package sbom

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestComponentJSONFields locks the CycloneDX field names this package
// emits (in particular "bom-ref", which Go's default JSON naming would
// never produce from a Go identifier without an explicit tag).
func TestComponentJSONFields(t *testing.T) {
	c := Component{
		BOMRef:  "pkg:golang/example.com/mod@v1.0.0",
		Type:    ComponentTypeLibrary,
		Name:    "example.com/mod",
		Version: "v1.0.0",
		PURL:    "pkg:golang/example.com/mod@v1.0.0",
		Scope:   ScopeRequired,
		Hashes:  []Hash{{Alg: HashAlgSHA256, Content: "ab"}},
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{`"bom-ref"`, `"type"`, `"name"`, `"version"`, `"purl"`, `"scope"`, `"hashes"`, `"alg"`, `"content"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("Component JSON missing key %s: %s", key, data)
		}
	}
}

// TestDocumentOmitsEmptyDependencies proves an all-defaults Document does
// not carry a "dependencies" key at all, so a BOM with nothing to say about
// the graph does not lie with an empty array.
func TestDocumentOmitsEmptyDependencies(t *testing.T) {
	doc := Document{BOMFormat: BOMFormatCycloneDX, SpecVersion: SpecVersion15, Version: 1}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), `"dependencies"`) {
		t.Errorf("expected no dependencies key on an empty Document, got %s", data)
	}
	if !strings.Contains(string(data), `"bomFormat":"CycloneDX"`) {
		t.Errorf("expected bomFormat field, got %s", data)
	}
	if !strings.Contains(string(data), `"specVersion":"1.5"`) {
		t.Errorf("expected specVersion field, got %s", data)
	}
}

func TestConstants(t *testing.T) {
	if BOMFormatCycloneDX != "CycloneDX" {
		t.Errorf("BOMFormatCycloneDX = %q", BOMFormatCycloneDX)
	}
	if SpecVersion15 != "1.5" {
		t.Errorf("SpecVersion15 = %q", SpecVersion15)
	}
	if HashAlgSHA256 != "SHA-256" {
		t.Errorf("HashAlgSHA256 = %q", HashAlgSHA256)
	}
	if ComponentTypeApplication != "application" || ComponentTypeLibrary != "library" {
		t.Errorf("component types = %q, %q", ComponentTypeApplication, ComponentTypeLibrary)
	}
	if ScopeRequired != "required" || ScopeOptional != "optional" || UnknownLicense != "UNKNOWN" {
		t.Errorf("scope/license constants = %q, %q, %q", ScopeRequired, ScopeOptional, UnknownLicense)
	}
}

func TestLicenseExceptionCompleteAndCovers(t *testing.T) {
	complete := LicenseException{Component: "example.com/mod", Version: "v1.0.0", Reason: "pending", Reviewer: "reviewer", Expiry: "2027-01-01"}
	if missing := complete.Complete(); len(missing) != 0 {
		t.Fatalf("complete exception missing = %v", missing)
	}
	if !complete.Covers(Component{Name: "example.com/mod", Version: "v1.0.0"}) {
		t.Fatal("exception did not cover the exact component revision")
	}
	if complete.Covers(Component{Name: "example.com/mod", Version: "v2.0.0"}) {
		t.Fatal("exception covered a different component revision")
	}
	missing := (LicenseException{}).Complete()
	if len(missing) != 5 || missing[0] != "component" || missing[4] != "expiry" {
		t.Fatalf("empty exception missing = %v, want all fields in order", missing)
	}
}
