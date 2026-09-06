package masking

import (
	"strings"
	"testing"
	"time"
)

var maskingNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func maskingFixture() (Dataset, Profile) {
	source := Dataset{SourceAuthority: "authority:promotion-fixture", SourceEnvironment: "SANDBOX", TenantID: "tenant-a", Purpose: "conformance", ProfileVersion: "profile-1", Rows: []SourceRow{
		{ID: "worker-1", Values: map[string]string{"name": "Alice Example", "birth_date": "1990-01-02", "salary": "1234.50", "manager_ref": "worker-manager", "state": "CA"}, SemanticTags: []string{"promotion", "decimal-edge", "legal-edge"}},
		{ID: "worker-2", Values: map[string]string{"name": "Bob Example", "birth_date": "1990-01-02", "salary": "0.00", "manager_ref": "worker-manager", "state": "CA"}, SemanticTags: []string{"promotion", "decimal-edge", "legal-edge"}},
	}}
	profile := Profile{Version: "profile-1", TenantID: "tenant-a", Purpose: "conformance", SourceAuthority: source.SourceAuthority, SourceEnvironment: source.SourceEnvironment, Secret: []byte("0123456789abcdef-secret"), ExpiresAt: maskingNow.Add(time.Hour), Fields: map[string]FieldRule{"name": {Kind: FieldDirectID}, "birth_date": {Kind: FieldTemporal}, "salary": {Kind: FieldDecimal}, "manager_ref": {Kind: FieldReference}, "state": {Kind: FieldLegal}}, RequiredTags: []string{"promotion", "decimal-edge", "legal-edge"}}
	return source, profile
}

func TestMaskedSyntheticDatasetPreventsReidentificationAndPreservesDeclaredSemanticCoverage(t *testing.T) {
	source, profile := maskingFixture()
	result, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Privacy.DirectIdentifiersRemoved || !result.Privacy.QuasiIdentifiersGeneralized || !result.Privacy.VisibleSyntheticMarker {
		t.Fatalf("privacy assessment failed: %+v", result.Privacy)
	}
	if result.Fidelity.PreservedTags != result.Fidelity.RequiredTags || result.Marker == "" || len(result.Rows) != len(source.Rows) {
		t.Fatalf("semantic coverage failed: %+v", result)
	}
	if result.Rows[0].Values["name"] == source.Rows[0].Values["name"] || result.Rows[0].Values["manager_ref"] == source.Rows[0].Values["manager_ref"] {
		t.Fatal("source identifiers survived")
	}
	if result.Rows[0].Values["manager_ref"] != result.Rows[1].Values["manager_ref"] {
		t.Fatal("referential equality was not preserved")
	}
}

func TestTodo_MASK_001_Property(t *testing.T) {
	source, profile := maskingFixture()
	one, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	if one.Digest != two.Digest {
		t.Fatal("same scoped input was not reproducible")
	}
}

func TestTodo_MASK_001_Golden(t *testing.T) {
	source, profile := maskingFixture()
	result, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Digest) != 64 || !strings.HasPrefix(result.Marker, "SYNTHETIC_DATASET/") {
		t.Fatalf("unexpected canonical result: %+v", result)
	}
}

func TestTodo_MASK_001_Integration(t *testing.T) {
	source, profile := maskingFixture()
	result, err := Generate(source, profile, maskingNow)
	if err != nil || result.Digest == "" {
		t.Fatalf("generation integration failed: %+v %v", result, err)
	}
}

func TestTodo_MASK_001_Fault(t *testing.T) {
	source, profile := maskingFixture()
	profile.ExpiresAt = maskingNow.Add(-time.Second)
	if _, err := Generate(source, profile, maskingNow); err != ErrExpired {
		t.Fatalf("expired profile error = %v", err)
	}
}

func TestTodo_MASK_001_Security(t *testing.T) {
	source, profile := maskingFixture()
	result, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	other := profile
	other.TenantID = "tenant-b"
	source.TenantID = "tenant-b"
	resultOther, err := Generate(source, other, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0].Values["name"] == resultOther.Rows[0].Values["name"] {
		t.Fatal("deterministic token crossed tenant scope")
	}
	source, profile = maskingFixture()
	source.SourceEnvironment = "PRODUCTION"
	profile.SourceEnvironment = "PRODUCTION"
	if _, err := Generate(source, profile, maskingNow); err == nil {
		t.Fatal("production source accepted")
	}
}

func TestTodo_MASK_001_Conformance(t *testing.T) {
	source, profile := maskingFixture()
	reversed := source
	reversed.Rows = []SourceRow{source.Rows[1], source.Rows[0]}
	a, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(reversed, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatal("row order changed semantic digest")
	}
}

func TestTodo_MASK_001_Recovery(t *testing.T) {
	source, profile := maskingFixture()
	result, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := Destroy(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Destroyed || len(result.Rows) != 0 || !result.Expired(maskingNow) {
		t.Fatalf("destroy recovery state invalid: %+v", result)
	}
}

func TestTodo_MASK_001_Mutation(t *testing.T) {
	source, profile := maskingFixture()
	base, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	profile.Fields["state"] = FieldRule{Kind: FieldPublic}
	changed, err := Generate(source, profile, maskingNow)
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest == changed.Digest || changed.Rows[0].Values["state"] != "CA" {
		t.Fatal("field transformation mutation was lost")
	}
}

func BenchmarkTodo_MASK_001(b *testing.B) {
	source, profile := maskingFixture()
	for i := 0; i < b.N; i++ {
		if _, err := Generate(source, profile, maskingNow); err != nil {
			b.Fatal(err)
		}
	}
}
