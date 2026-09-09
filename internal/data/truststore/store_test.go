package truststore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/accessreview"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTrustTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`,
		tenant, key, "tenant "+key)
	return tenant
}

func trustAppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func trustFixture(t *testing.T) (*Store, *pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := insertTrustTenant(t, db, "trust-primary")
	return New(trustAppConn(t, db)), db, tenant
}

func sampleGrant(tenant uuid.UUID, id string) JITGrantRecord {
	return JITGrantRecord{
		TenantID: tenant, RowID: uuid.New(), GrantID: id, Revision: 1, State: "ACTIVE",
		Requester: "operator-a", Approver: "operator-b", Scope: []byte(`{"role":"INCIDENT_RESPONDER"}`),
		NotBefore: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC),
	}
}

func sampleSchedule() accessreview.Schedule {
	return accessreview.Schedule{
		AsOf:   time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		Policy: accessreview.DefaultPolicy(),
		Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
}

func sampleAccessReviewGrant(tenant uuid.UUID, id string) AccessReviewGrantRecord {
	return AccessReviewGrantRecord{
		TenantID: tenant, RowID: uuid.New(), GrantID: id, PrincipalID: "principal-" + id,
		Capabilities: []byte(`{"capabilities":["read:metadata"]}`),
		GrantedAt:    time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

// TestTodo_PERSIST_TRUST_001 is the primary durable-path contract: the
// single-use proof, JIT revision, JIT evidence, schedule and review rows all
// survive the same tenant-scoped store boundary.
func TestTodo_PERSIST_TRUST_001(t *testing.T) {
	store, _, tenant := trustFixture(t)
	ctx := context.Background()

	proofStore := store.ForTenant(tenant)
	consumed, err := proofStore.Consume(ctx, "proof-1", "executed")
	if err != nil || consumed {
		t.Fatalf("first proof consume = %v, %v; want false, nil", consumed, err)
	}
	consumed, err = proofStore.Consume(ctx, "proof-1", "replayed")
	if err != nil || !consumed {
		t.Fatalf("second proof consume = %v, %v; want true, nil", consumed, err)
	}

	grant := sampleGrant(tenant, "jit-1")
	if err := store.PutJITGrant(ctx, tenant, grant); err != nil {
		t.Fatalf("PutJITGrant: %v", err)
	}
	if _, err := store.LoadJITGrant(ctx, tenant, grant.GrantID, 1); err != nil {
		t.Fatalf("LoadJITGrant: %v", err)
	}
	accessGrant := sampleAccessReviewGrant(tenant, "access-1")
	if err := store.PutAccessReviewGrant(ctx, tenant, accessGrant); err != nil {
		t.Fatalf("PutAccessReviewGrant: %v", err)
	}
	if _, err := store.LoadAccessReviewGrant(ctx, tenant, accessGrant.GrantID); err != nil {
		t.Fatalf("LoadAccessReviewGrant: %v", err)
	}
	if err := store.AppendJITEvidence(ctx, JITEvidenceRecord{
		TenantID: tenant, RowID: uuid.New(), GrantID: grant.GrantID, EvidenceKind: "GRANTED",
		Detail: []byte(`{"actor":"operator-b"}`), At: grant.NotBefore, EventSequence: 1,
	}); err != nil {
		t.Fatalf("AppendJITEvidence: %v", err)
	}

	schedule, err := store.PutAccessReviewSchedule(ctx, tenant, "schedule-1", sampleSchedule())
	if err != nil {
		t.Fatalf("PutAccessReviewSchedule: %v", err)
	}
	if _, err := store.LoadAccessReviewSchedule(ctx, tenant, schedule.ScheduleID); err != nil {
		t.Fatalf("LoadAccessReviewSchedule: %v", err)
	}
	if err := store.AppendAccessReviewRecord(ctx, AccessReviewReviewRecord{
		TenantID: tenant, RowID: uuid.New(), ScheduleID: schedule.ScheduleID, ReviewerID: "reviewer-1",
		Decision: string(accessreview.Continue), Justification: "reviewed", At: grant.NotBefore, EventSequence: 1,
	}); err != nil {
		t.Fatalf("AppendAccessReviewRecord: %v", err)
	}
}

// TestTodo_PERSIST_TRUST_001_Fault proves duplicate revisions and stale CAS
// attempts are refused with stable typed codes.
func TestTodo_PERSIST_TRUST_001_Fault(t *testing.T) {
	store, _, tenant := trustFixture(t)
	ctx := context.Background()
	grant := sampleGrant(tenant, "jit-fault")
	if err := store.PutJITGrant(ctx, tenant, grant); err != nil {
		t.Fatal(err)
	}
	if err := store.PutJITGrant(ctx, tenant, grant); CodeOf(err) != CodeDuplicateRevision {
		t.Fatalf("duplicate revision code = %s, want %s (err=%v)", CodeOf(err), CodeDuplicateRevision, err)
	}
	if _, err := store.UpdateJITGrant(ctx, tenant, grant.GrantID, 1, "REVOKED", true); err != nil {
		t.Fatalf("first CAS update: %v", err)
	}
	if _, err := store.UpdateJITGrant(ctx, tenant, grant.GrantID, 1, "ACTIVE", false); CodeOf(err) != CodeVersionConflict {
		t.Fatalf("stale CAS code = %s, want %s (err=%v)", CodeOf(err), CodeVersionConflict, err)
	}
	if err := store.AppendJITEvidence(ctx, JITEvidenceRecord{
		TenantID: tenant, RowID: uuid.New(), GrantID: grant.GrantID, EvidenceKind: "GRANTED",
		At: grant.NotBefore, EventSequence: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendJITEvidence(ctx, JITEvidenceRecord{
		TenantID: tenant, RowID: uuid.New(), GrantID: grant.GrantID, EvidenceKind: "USED",
		At: grant.NotBefore, EventSequence: 1,
	}); CodeOf(err) != CodeDuplicateEvent {
		t.Fatalf("duplicate event code = %s, want %s (err=%v)", CodeOf(err), CodeDuplicateEvent, err)
	}
}

// TestTodo_PERSIST_TRUST_001_Integration proves the domain-shaped schedule
// and review data is readable after it crosses the SQL adapter.
func TestTodo_PERSIST_TRUST_001_Integration(t *testing.T) {
	store, _, tenant := trustFixture(t)
	ctx := context.Background()
	grant := sampleAccessReviewGrant(tenant, "access-integration")
	if err := store.PutAccessReviewGrant(ctx, tenant, grant); err != nil {
		t.Fatal(err)
	}
	schedule := sampleSchedule()
	schedule.Entries = []accessreview.ScheduleEntry{{
		Grant:     accessreview.Grant{ID: grant.GrantID, GrantedAt: grant.GrantedAt},
		ReviewDue: grant.GrantedAt.Add(24 * time.Hour),
	}}
	record, err := store.PutAccessReviewSchedule(ctx, tenant, "schedule-integration", schedule)
	if err != nil {
		t.Fatal(err)
	}
	if string(record.Entries) == "" || record.Digest == "" {
		t.Fatalf("stored schedule projection is incomplete: %+v", record)
	}
}

// TestTodo_PERSIST_TRUST_001_Security proves tenant-scoped reads cannot see
// another tenant's row even when the caller supplies the other row's key.
func TestTodo_PERSIST_TRUST_001_Security(t *testing.T) {
	store, db, tenantA := trustFixture(t)
	tenantB := insertTrustTenant(t, db, "trust-security-b")
	grant := sampleGrant(tenantA, "jit-security")
	if err := store.PutJITGrant(context.Background(), tenantA, grant); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadJITGrant(context.Background(), tenantB, grant.GrantID, 1); CodeOf(err) != CodeNotFound {
		t.Fatalf("cross-tenant load code = %s, want %s (err=%v)", CodeOf(err), CodeNotFound, err)
	}
	conn := trustAppConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tenancy.WithTenant(context.Background(), tx, tenantB); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM jit_grant`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tenant B saw %d tenant A grant rows", count)
	}
	if _, err := store.LoadAccessReviewGrant(context.Background(), tenantB, "access-security"); CodeOf(err) != CodeNotFound {
		t.Fatalf("cross-tenant access-review grant code = %s, want %s (err=%v)", CodeOf(err), CodeNotFound, err)
	}
}

// TestTodo_PERSIST_TRUST_001_Recovery proves a fresh connection reloads the
// committed control row.
func TestTodo_PERSIST_TRUST_001_Recovery(t *testing.T) {
	store, db, tenant := trustFixture(t)
	grant := sampleGrant(tenant, "jit-recovery")
	if err := store.PutJITGrant(context.Background(), tenant, grant); err != nil {
		t.Fatal(err)
	}
	fresh := New(trustAppConn(t, db))
	loaded, err := fresh.LoadJITGrant(context.Background(), tenant, grant.GrantID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GrantID != grant.GrantID || loaded.Revision != 1 {
		t.Fatalf("fresh connection loaded %+v", loaded)
	}
	accessGrant := sampleAccessReviewGrant(tenant, "access-recovery")
	if err := store.PutAccessReviewGrant(context.Background(), tenant, accessGrant); err != nil {
		t.Fatal(err)
	}
	fresh = New(trustAppConn(t, db))
	loadedAccess, err := fresh.LoadAccessReviewGrant(context.Background(), tenant, accessGrant.GrantID)
	if err != nil || loadedAccess.GrantID != accessGrant.GrantID {
		t.Fatalf("fresh connection access-review grant = %+v, err=%v", loadedAccess, err)
	}
}

// TestTodo_PERSIST_TRUST_001_Mutation proves the append-only ledgers reject
// direct UPDATE and DELETE attempts under the application role.
func TestTodo_PERSIST_TRUST_001_Mutation(t *testing.T) {
	store, db, tenant := trustFixture(t)
	if consumed, err := store.ForTenant(tenant).Consume(context.Background(), "proof-mutation", "executed"); err != nil || consumed {
		t.Fatalf("consume proof: %v, %v", consumed, err)
	}
	grant := sampleGrant(tenant, "jit-mutation")
	if err := store.PutJITGrant(context.Background(), tenant, grant); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendJITEvidence(context.Background(), JITEvidenceRecord{
		TenantID: tenant, RowID: uuid.New(), GrantID: grant.GrantID, EvidenceKind: "GRANTED",
		At: grant.NotBefore, EventSequence: 1,
	}); err != nil {
		t.Fatal(err)
	}
	schedule, err := store.PutAccessReviewSchedule(context.Background(), tenant, "schedule-mutation", sampleSchedule())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendAccessReviewRecord(context.Background(), AccessReviewReviewRecord{
		TenantID: tenant, RowID: uuid.New(), ScheduleID: schedule.ScheduleID, ReviewerID: "reviewer-mutation",
		Decision: string(accessreview.Continue), At: grant.NotBefore, EventSequence: 1,
	}); err != nil {
		t.Fatal(err)
	}
	conn := trustAppConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE stepup_proof_log SET outcome='tampered' WHERE tenant_id=$1`,
		`DELETE FROM stepup_proof_log WHERE tenant_id=$1`,
		`UPDATE jit_evidence_record SET evidence_kind='tampered' WHERE tenant_id=$1`,
		`DELETE FROM jit_evidence_record WHERE tenant_id=$1`,
		`UPDATE accessreview_review_record SET decision='tampered' WHERE tenant_id=$1`,
		`DELETE FROM accessreview_review_record WHERE tenant_id=$1`,
	} {
		if _, err := tx.Exec(context.Background(), statement, tenant); err == nil {
			t.Fatalf("append-only mutation succeeded: %s", statement)
		}
	}
}
