package custom

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func seedSearchStore(t *testing.T) (*EventStore, values.TenantId) {
	t.Helper()
	store, tenant := NewEventStore(), values.TenantId("tenant-a")
	for i, registration := range []string{"ABC-1", "ABC-2"} {
		req := testMutation(t, tenant, "vehicle-"+string(rune('1'+i)), registration, OperationCreate, 0, 0)
		if _, err := store.Commit(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	return store, tenant
}

// TestTodo_CUSTOM_006 verifies exact authorized equality search and freshness
// plus schema metadata in the report.
func TestTodo_CUSTOM_006(t *testing.T) {
	store, tenant := seedSearchStore(t)
	report, err := store.Search(context.Background(), SearchRequest{Tenant: tenant, Definition: testDefinition(), Query: map[string]string{"registration": "ABC-2"}, Purpose: "fleet.operations", GrantedDomains: map[string]bool{"worker.core": true}, RequestedFields: []string{"registration"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Count != 1 || report.SourceHead != 1 || report.ProjectionSequence != 1 || report.SchemaVersion != 1 || report.Records[0].ObjectID != "vehicle-2" {
		t.Fatalf("search report = %+v", report)
	}
}

// TestTodo_CUSTOM_006_Golden proves map and result ordering are deterministic.
func TestTodo_CUSTOM_006_Golden(t *testing.T) {
	store, tenant := seedSearchStore(t)
	req := SearchRequest{Tenant: tenant, Definition: testDefinition(), Purpose: "fleet.operations", GrantedDomains: map[string]bool{"worker.core": true}, RequestedFields: []string{"active", "registration"}}
	a, err := store.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || a.Records[0].ObjectID != "vehicle-1" || a.Records[1].ObjectID != "vehicle-2" {
		t.Fatalf("non-deterministic report: %+v %+v", a, b)
	}
}

// TestTodo_CUSTOM_006_Recovery rejects an authorization request for a
// restricted field rather than leaking a count or a placeholder value.
func TestTodo_CUSTOM_006_Recovery(t *testing.T) {
	store, tenant := seedSearchStore(t)
	if _, err := store.Search(context.Background(), SearchRequest{Tenant: tenant, Definition: testDefinition(), Purpose: "fleet.operations", GrantedDomains: map[string]bool{"worker.core": true}, MinimumSequence: 2}); err == nil {
		t.Fatal("stale projection was presented as an empty successful result")
	}
	def := testDefinition()
	def.Fields["bank_account"] = FieldDefinition{Type: "string", Classification: FieldClassification{AuthZDomain: "worker.bank", Classification: "CONFIDENTIAL", ResidencyRef: "PCI_DSS", RetentionClass: "PERMANENT"}}
	if _, err := store.Search(context.Background(), SearchRequest{Tenant: tenant, Definition: def, Purpose: "fleet.operations", GrantedDomains: map[string]bool{"worker.core": true}, RequestedFields: []string{"bank_account"}}); err == nil {
		t.Fatal("restricted field was exposed")
	}
}
