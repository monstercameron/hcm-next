package iac

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func catalogFixture() Catalog {
	kinds := []string{ResourceNetwork, ResourceCompute, ResourcePostgres, ResourceObject, ResourceQueue, ResourceCache, ResourceTelemetry, ResourceSecrets, ResourceEdge}
	resources := make([]Resource, 0, len(kinds))
	for i, kind := range kinds {
		resources = append(resources, Resource{ID: "cell-" + kind, Kind: kind, Encryption: true, Backup: true, Owner: "platform", Region: "region-a", Residency: "eu", Classification: "internal", SLO: "99.9%", BackupPolicy: "daily-and-tested", Cost: "budgeted", Capacity: "bounded-and-scalable", ReplacementStrategy: "recreate-with-cutover"})
		resources[i].Tags = map[string]string{"owner": "platform", "environment": "test", "data_classification": "internal"}
		if kind == ResourceCompute {
			resources[i].ImageDigest = "sha256:" + strings.Repeat("a", 64)
			resources[i].ImageSignatureVerified = true
		}
	}
	return Catalog{Version: 1, Resources: resources}
}

func TestTodo_IAC_001(t *testing.T) {
	if report := ValidateCatalog(catalogFixture()); !report.OK() {
		t.Fatalf("complete resource catalog rejected: %+v", report.Findings)
	}
	if b, err := json.Marshal(catalogFixture()); err != nil || !json.Valid(b) || !strings.Contains(string(b), `"postgres"`) {
		t.Fatalf("catalog is not machine-readable: %s (%v)", b, err)
	}
}

func TestTodo_IAC_001_Race(t *testing.T) {
	catalog := catalogFixture()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !ValidateCatalog(catalog).OK() {
				t.Error("concurrent validation rejected stable catalog")
			}
		}()
	}
	wg.Wait()
}

func TestTodo_IAC_001_Integration(t *testing.T) {
	input, err := json.Marshal(catalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	var decoded Catalog
	if err := json.Unmarshal(input, &decoded); err != nil {
		t.Fatal(err)
	}
	if report := ValidateCatalog(decoded); !report.OK() {
		t.Fatalf("serialized catalog failed validation: %+v", report.Findings)
	}
	if report := Validate(Input{Resources: decoded.Resources}); !report.OK() {
		t.Fatalf("catalog resources failed IAC-003 policy integration: %+v", report.Findings)
	}
}

func TestTodo_IAC_001_Fault(t *testing.T) {
	tests := []struct {
		field string
		clear func(*Resource)
	}{
		{"owner", func(r *Resource) { r.Owner = "\t" }},
		{"region", func(r *Resource) { r.Region = " " }},
		{"residency", func(r *Resource) { r.Residency = "" }},
		{"classification", func(r *Resource) { r.Classification = "" }},
		{"slo", func(r *Resource) { r.SLO = "" }},
		{"backup_policy", func(r *Resource) { r.BackupPolicy = "" }},
		{"cost", func(r *Resource) { r.Cost = "" }},
		{"capacity", func(r *Resource) { r.Capacity = "" }},
		{"replacement_strategy", func(r *Resource) { r.ReplacementStrategy = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			c := catalogFixture()
			tt.clear(&c.Resources[0])
			report := ValidateCatalog(c)
			if !hasCatalogFinding(report, "MISSING_CONTRACT_FIELD", tt.field) {
				t.Errorf("missing fault for %s: %+v", tt.field, report.Findings)
			}
		})
	}
}

func TestTodo_IAC_001_Security(t *testing.T) {
	for _, invalidKind := range []string{"aws_network", " network ", "NETWORK", "", "\t"} {
		c := catalogFixture()
		c.Resources[0].Kind = invalidKind
		if report := ValidateCatalog(c); report.OK() {
			t.Fatalf("unknown or non-canonical kind %q was accepted", invalidKind)
		}
	}
	c := catalogFixture()
	c.Resources[1].ID = "  " + c.Resources[0].ID + "\t"
	if report := ValidateCatalog(c); !hasCatalogFinding(report, "DUPLICATE_ID", "id") {
		t.Fatalf("whitespace-obscured duplicate id was accepted: %+v", report.Findings)
	}
}

func TestTodo_IAC_001_Recovery(t *testing.T) {
	c := catalogFixture()
	c.Resources[2].BackupPolicy = ""
	c.Resources[2].ReplacementStrategy = ""
	if report := ValidateCatalog(c); !hasCatalogFinding(report, "MISSING_CONTRACT_FIELD", "backup_policy") || !hasCatalogFinding(report, "MISSING_CONTRACT_FIELD", "replacement_strategy") {
		t.Fatalf("unrecoverable postgres resource was accepted: %+v", report.Findings)
	}
	c.Resources[2].BackupPolicy = "verified-restore"
	c.Resources[2].ReplacementStrategy = "restore-then-cutover"
	if report := ValidateCatalog(c); !report.OK() {
		t.Fatalf("recoverable postgres catalog rejected: %+v", report.Findings)
	}
}

func hasCatalogFinding(report CatalogReport, code, field string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code && finding.Field == field {
			return true
		}
	}
	return false
}

func BenchmarkTodo_IAC_001(b *testing.B) {
	c := catalogFixture()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !ValidateCatalog(c).OK() {
			b.Fatal("fixture rejected")
		}
	}
}
