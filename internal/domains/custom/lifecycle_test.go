package custom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func testDefinition() CustomObjectDefinition {
	return CustomObjectDefinition{Kind: "Vehicle", Namespace: "tenant.fleet", Version: 1, Fields: map[string]FieldDefinition{
		"registration": {Type: "string", Classification: FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}},
		"active":       {Type: "bool", Classification: FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}},
	}}
}

func testRecord(t *testing.T, id, registration string, active bool) CustomRecordRevision {
	t.Helper()
	at := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	interval, err := values.NewOpenInstantInterval(at)
	if err != nil {
		t.Fatal(err)
	}
	return CustomRecordRevision{ObjectID: id, ObjectKind: "Vehicle", Namespace: "tenant.fleet", DefinitionVersion: 1, Effective: interval,
		FieldValues: map[string]TypedValue{"registration": {FieldName: "registration", Type: "string", Value: registration}, "active": {FieldName: "active", Type: "bool", Value: active}}}
}

func testMutation(t *testing.T, tenant values.TenantId, id, registration string, operation Operation, head int64, revision uint64) MutationRequest {
	return MutationRequest{Tenant: tenant, ObjectID: id, Definition: testDefinition(), Record: testRecord(t, id, registration, true), Operation: operation,
		ExpectedHead: head, ExpectedRevision: revision, Actor: "actor-1", Purpose: "fleet.operations", EvidenceDigest: "evidence-1",
		GrantedDomains: map[string]bool{"worker.core": true}, OccurredAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
}

// TestTodo_CUSTOM_004 verifies the canonical append/projection/outbox commit
// and compare-and-swap behavior for the custom-object lifecycle.
func TestTodo_CUSTOM_004(t *testing.T) {
	store := NewEventStore()
	tenant, objectID := values.TenantId("tenant-a"), "vehicle-1"
	created, err := store.Commit(context.Background(), testMutation(t, tenant, objectID, "ABC-1", OperationCreate, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	changed, err := store.Commit(context.Background(), testMutation(t, tenant, objectID, "ABC-2", OperationChange, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if created.Event.Type != EventObjectCreated || changed.Event.Type != EventObjectChanged || changed.Event.Revision != 2 {
		t.Fatalf("unexpected lifecycle events: %+v %+v", created.Event, changed.Event)
	}
	if len(store.Outbox()) != 2 {
		t.Fatalf("outbox count = %d, want 2", len(store.Outbox()))
	}
	report, err := store.Rebuild(context.Background(), tenant, "Vehicle", objectID)
	if err != nil {
		t.Fatal(err)
	}
	if report.SourceHead != 2 || report.SchemaVersion != 1 || report.Digest != changed.Event.Digest {
		t.Fatalf("rebuild report = %+v", report)
	}
}

// TestTodo_CUSTOM_004_Recovery proves a detached event read can be inspected
// without permitting callers to mutate the live projection.
func TestTodo_CUSTOM_004_Recovery(t *testing.T) {
	store := NewEventStore()
	tenant := values.TenantId("tenant-a")
	if _, err := store.Commit(context.Background(), testMutation(t, tenant, "vehicle-1", "ABC-1", OperationCreate, 0, 0)); err != nil {
		t.Fatal(err)
	}
	events := store.Events(tenant, "Vehicle", "vehicle-1")
	events[0].Digest = "tampered"
	report, err := store.Rebuild(context.Background(), tenant, "Vehicle", "vehicle-1")
	if err != nil || report.SourceHead != 1 {
		t.Fatalf("detached replay changed store: report=%+v err=%v", report, err)
	}
}

// TestTodo_CUSTOM_004_Mutation ensures a stale append is rejected before any
// event, projection, or outbox row is added.
func TestTodo_CUSTOM_004_Mutation(t *testing.T) {
	store := NewEventStore()
	tenant := values.TenantId("tenant-a")
	if _, err := store.Commit(context.Background(), testMutation(t, tenant, "vehicle-1", "ABC-1", OperationCreate, 0, 0)); err != nil {
		t.Fatal(err)
	}
	_, err := store.Commit(context.Background(), testMutation(t, tenant, "vehicle-1", "ABC-2", OperationChange, 0, 1))
	if !errors.Is(err, ErrStaleHead) {
		t.Fatalf("stale append error = %v", err)
	}
	if len(store.Events(tenant, "Vehicle", "vehicle-1")) != 1 || len(store.Outbox()) != 1 {
		t.Fatal("stale append partially committed")
	}
}
