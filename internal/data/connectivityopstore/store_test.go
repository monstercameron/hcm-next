package connectivityopstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func storeTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-store', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, key, key)
	return tenant
}

func storeOperation(t *testing.T, db *pgtest.DB, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	system, connector, connection, id := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	digest := strings.Repeat("1", 64)
	db.Exec(t, `INSERT INTO external_system
		(tenant_id, system_id, system_key, vendor, product, environment, residency_region, data_classification, owner_principal_ref)
		VALUES ($1, $2, 'system-store', 'vendor', 'product', 'TEST', 'us-east-1', 'PUBLIC', 'owner')`, tenant, system)
	db.Exec(t, `INSERT INTO connector_definition
		(tenant_id, connector_id, connector_key, connector_version, vendor, auth_mode, write_mode,
		 idempotency_semantics, observation_semantics, descriptor_digest)
		VALUES ($1, $2, 'connector-store', 1, 'vendor', 'NONE', 'IDEMPOTENT', 'KEY', 'READ_BACK', $3)`, tenant, connector, digest)
	db.Exec(t, `INSERT INTO connector_connection
		(tenant_id, connection_id, system_id, connector_id, environment, endpoint, credential_ref, residency_region)
		VALUES ($1, $2, $3, $4, 'TEST', 'https://api.example.test', 'secretref://store/test', 'us-east-1')`, tenant, connection, system, connector)
	db.Exec(t, `INSERT INTO connector_operation
		(tenant_id, operation_id, connection_id, sequence_no, effect_ref, workflow_ref, semantic_operation,
		 resource_key, canonical_payload_digest, mapped_payload_digest, idempotency_key, authority_digest,
		 classification, deadline_at)
		VALUES ($1, $2, $3, 1, 'effect:store', 'workflow:store', 'store.write', 'resource:1', $4, $4,
		 'idempotency:store', $4, 'PUBLIC', timestamptz '2026-09-07T00:00:00Z')`, tenant, id, connection, digest)
	return id
}

func TestStore_QueueClaim_IsTenantScopedAndFenced(t *testing.T) {
	db := pgtest.New(t)
	alpha := storeTenant(t, db, "store-alpha")
	beta := storeTenant(t, db, "store-beta")
	op := storeOperation(t, db, alpha)
	store := New(db.Conn)
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	if err := store.Enqueue(context.Background(), alpha, op, QueueItem{AvailableAt: at}); err != nil {
		t.Fatal(err)
	}
	lease, err := store.Claim(context.Background(), alpha, op, "worker-a", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease.TenantID != alpha.String() || lease.OperationID != op || lease.FenceToken != 1 || lease.Token == uuid.Nil {
		t.Fatalf("lease = %+v", lease)
	}
	if _, err := store.Claim(context.Background(), beta, op, "worker-b", at, time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant claim err = %v, want ErrNotFound", err)
	}
	if _, err := store.Claim(context.Background(), alpha, op, "worker-b", at.Add(time.Second), time.Minute); !errors.Is(err, ErrLeaseFenced) {
		t.Fatalf("live lease err = %v, want ErrLeaseFenced", err)
	}
	reclaimed, err := store.Claim(context.Background(), alpha, op, "worker-b", at.Add(2*time.Minute), time.Minute)
	if err != nil || reclaimed.FenceToken != 2 {
		t.Fatalf("expired lease reclaim = %+v, err=%v", reclaimed, err)
	}
}

func TestStore_Journal_IsAppendOnlyAndTenantScoped(t *testing.T) {
	db := pgtest.New(t)
	alpha := storeTenant(t, db, "journal-alpha")
	beta := storeTenant(t, db, "journal-beta")
	op := storeOperation(t, db, alpha)
	store := New(db.Conn)
	event := operation.JournalEvent{OperationID: op, TenantID: alpha.String(), OperationSequence: 1, Event: "PLANNED", To: operation.StatePlanned, OccurredAt: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC), Digest: strings.Repeat("2", 64)}
	if err := store.AppendJournal(context.Background(), alpha, event); err != nil {
		t.Fatal(err)
	}
	got, err := store.Journal(context.Background(), alpha, op)
	if err != nil || len(got) != 1 || got[0].OperationID != op || got[0].TenantID != alpha.String() {
		t.Fatalf("journal = %+v, err=%v", got, err)
	}
	foreign, err := store.Journal(context.Background(), beta, op)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("cross-tenant journal = %+v, err=%v", foreign, err)
	}
	if _, err := db.Conn.Exec(context.Background(), `UPDATE connector_operation_journal SET event_kind = 'TAMPERED' WHERE tenant_id = $1 AND operation_id = $2`, alpha, op); err == nil {
		t.Fatal("journal update unexpectedly succeeded")
	}
	if _, err := db.Conn.Exec(context.Background(), `DELETE FROM connector_operation_journal WHERE tenant_id = $1 AND operation_id = $2`, alpha, op); err == nil {
		t.Fatal("journal delete unexpectedly succeeded")
	}
}

func TestStore_CredentialBinding_IsReferenceOnly(t *testing.T) {
	db := pgtest.New(t)
	tenant := storeTenant(t, db, "binding-alpha")
	op := storeOperation(t, db, tenant)
	store := New(db.Conn)
	boundAt := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	binding := CredentialBinding{BindingID: uuid.New(), OperationID: op, CredentialLeaseRef: "lease-ref-1", WorkloadRef: "worker-ref-1", DestinationRef: "api.example.test", Purpose: "store.write", CustodyOperation: "encrypt", LeaseExpiresAt: boundAt.Add(time.Minute), RevocationEpoch: 4, Outcome: "BOUND", BoundAt: boundAt}
	if err := store.BindCredentialLease(context.Background(), tenant, binding); err != nil {
		t.Fatal(err)
	}
	got, err := store.CredentialBindings(context.Background(), tenant, op)
	if err != nil || len(got) != 1 || got[0].CredentialLeaseRef != binding.CredentialLeaseRef || got[0].RevocationEpoch != 4 {
		t.Fatalf("binding = %+v, err=%v", got, err)
	}
	serialized, err := json.Marshal(got[0])
	if err != nil || strings.Contains(string(serialized), "secret-value") || strings.Contains(string(serialized), "raw-secret") {
		t.Fatalf("serialized binding contains secret material: %s", serialized)
	}
	foreign, err := store.CredentialBindings(context.Background(), uuid.New(), op)
	if err != nil || len(foreign) != 0 {
		t.Fatalf("unexpected foreign binding result: %+v, err=%v", foreign, err)
	}
}
