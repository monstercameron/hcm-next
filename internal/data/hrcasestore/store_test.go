package hrcasestore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/hrcasestore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/hrcase"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func fixtureRevision(t *testing.T, caseID string) hrcase.CaseRevision {
	t.Helper()
	definition, err := hrcase.NewCaseDefinition(hrcase.CaseDefinition{
		Type: "BENEFITS", Version: "2026-01", Service: "leave", Purpose: "case handling",
		Classification: "CONFIDENTIAL", Retention: "PERMANENT",
	})
	if err != nil {
		t.Fatalf("NewCaseDefinition: %v", err)
	}
	request, err := hrcase.NewHRRequest(hrcase.HRRequest{
		CaseID: caseID, Type: definition.Type, TypeVersion: definition.Version, Service: definition.Service,
		Requester: "principal:requester", Subject: "worker:subject", Purpose: definition.Purpose,
		Classification: definition.Classification, Retention: definition.Retention,
		Participants: []hrcase.Participant{{Role: "REQUESTER", Principal: "principal:requester"}},
	})
	if err != nil {
		t.Fatalf("NewHRRequest: %v", err)
	}
	revision, err := request.Revision()
	if err != nil {
		t.Fatalf("Revision: %v", err)
	}
	return revision
}

func appendRevision(t *testing.T, store *hrcasestore.Store, tenant uuid.UUID, revision hrcase.CaseRevision, sequence uint64) {
	t.Helper()
	if err := store.AppendRevision(context.Background(), tenant.String(), revision, sequence); err != nil {
		t.Fatalf("AppendRevision %d: %v", sequence, err)
	}
}

// TestTodo_PERSIST_HRCASE_001 proves a complete append and governed current
// state read against the real migration.
func TestTodo_PERSIST_HRCASE_001(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "hrcase-primary")
	store := hrcasestore.New(appConn(t, db))
	first := fixtureRevision(t, "case-primary")
	appendRevision(t, store, tenant, first, 1)

	got, err := store.Current(context.Background(), tenant.String(), first.CaseID)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got.Revision != 1 || got.State != hrcase.Draft || !got.Verify() {
		t.Fatalf("Current = %+v, want verified DRAFT revision one", got)
	}
}

// TestTodo_PERSIST_HRCASE_001_Integration proves the domain lifecycle can be
// replayed from the persisted stream without a mutable status row.
func TestTodo_PERSIST_HRCASE_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "hrcase-integration")
	store := hrcasestore.New(appConn(t, db))
	aggregate, err := hrcase.NewCase(hrcase.HRRequest{
		CaseID: "case-integration", Type: "BENEFITS", TypeVersion: "2026-01", Service: "leave",
		Requester: "principal:requester", Subject: "worker:subject", Purpose: "case handling",
		Classification: "CONFIDENTIAL", Retention: "PERMANENT",
		Participants: []hrcase.Participant{{Role: "REQUESTER", Principal: "principal:requester"}},
	})
	if err != nil {
		t.Fatalf("NewCase: %v", err)
	}
	appendRevision(t, store, tenant, aggregate.Current(), 1)
	for _, step := range []struct {
		state       hrcase.CaseState
		disposition string
	}{{hrcase.Open, ""}, {hrcase.Waiting, "awaiting response"}, {hrcase.Paused, "calendar paused"}, {hrcase.Open, "resumed"}, {hrcase.Resolved, "resolved"}, {hrcase.Closed, "closed"}} {
		if _, err := aggregate.Apply(step.state, step.disposition); err != nil {
			t.Fatalf("Apply %s: %v", step.state, err)
		}
		appendRevision(t, store, tenant, aggregate.Current(), aggregate.Current().Revision)
	}
	events, err := store.ListRevisions(context.Background(), tenant.String(), aggregate.Current().CaseID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(events) != 7 || events[0].State != hrcase.Draft || events[len(events)-1].State != hrcase.Closed {
		t.Fatalf("persisted stream = %+v, want 7 revisions ending CLOSED", events)
	}
	for i, event := range events {
		if event.Revision != uint64(i+1) || event.Previous != uint64(i) {
			t.Fatalf("stream event %d lineage = revision %d previous %d", i, event.Revision, event.Previous)
		}
	}
}

// TestTodo_PERSIST_HRCASE_001_Security proves RLS hides another tenant's
// complete case stream, including the absence/presence distinction.
func TestTodo_PERSIST_HRCASE_001_Security(t *testing.T) {
	db := pgtest.New(t)
	owner := insertTenant(t, db, "hrcase-owner")
	other := insertTenant(t, db, "hrcase-other")
	store := hrcasestore.New(appConn(t, db))
	first := fixtureRevision(t, "case-secret")
	appendRevision(t, store, owner, first, 1)

	if _, err := store.Current(context.Background(), other.String(), first.CaseID); !errors.Is(err, hrcase.ErrStoreNotFound) {
		t.Fatalf("cross-tenant Current error = %v, want ErrStoreNotFound", err)
	}
	conn := appConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, other); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("WithTenant: %v", err)
	}
	var count int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM hr_case_revision`).Scan(&count); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatalf("RLS count: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant RLS count = %d, want zero", count)
	}
}

// TestTodo_PERSIST_HRCASE_001_Recovery proves a fresh connection reloads the
// durable stream.
func TestTodo_PERSIST_HRCASE_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "hrcase-recovery")
	first := fixtureRevision(t, "case-recovery")
	appendRevision(t, hrcasestore.New(appConn(t, db)), tenant, first, 1)

	store := hrcasestore.New(appConn(t, db))
	got, err := store.LoadRevision(context.Background(), tenant.String(), first.CaseID, 1)
	if err != nil {
		t.Fatalf("LoadRevision on fresh connection: %v", err)
	}
	if got.Digest != first.Digest || got.Participants[0] != first.Participants[0] {
		t.Fatalf("reloaded revision = %+v, want original durable content", got)
	}
}

// TestTodo_PERSIST_HRCASE_001_Fault proves duplicate and stale-CAS refusals
// carry stable typed domain codes.
func TestTodo_PERSIST_HRCASE_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "hrcase-fault")
	store := hrcasestore.New(appConn(t, db))
	first := fixtureRevision(t, "case-fault")
	appendRevision(t, store, tenant, first, 1)
	if err := store.AppendRevision(context.Background(), tenant.String(), first, 1); !errors.Is(err, hrcase.ErrStoreDuplicate) {
		t.Fatalf("duplicate error = %v, want ErrStoreDuplicate", err)
	}
	next, err := hrcase.Apply(first, hrcase.Open, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	err = store.AppendRevision(context.Background(), tenant.String(), next.Revision, 3)
	if !errors.Is(err, hrcase.ErrStoreStaleCAS) {
		t.Fatalf("stale error = %v, want ErrStoreStaleCAS", err)
	}
	var typed *hrcase.StoreError
	if !errors.As(err, &typed) || typed.Code != hrcase.StoreStaleCASCode {
		t.Fatalf("stale error = %T/%v, want typed %s", err, err, hrcase.StoreStaleCASCode)
	}
}

// TestTodo_PERSIST_HRCASE_001_Mutation proves the database trigger refuses
// both update and delete of the append-only ledger row.
func TestTodo_PERSIST_HRCASE_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "hrcase-mutation")
	store := hrcasestore.New(appConn(t, db))
	first := fixtureRevision(t, "case-mutation")
	appendRevision(t, store, tenant, first, 1)

	var rowID uuid.UUID
	if err := db.QueryRow(context.Background(), `SELECT row_id FROM hr_case_revision WHERE tenant_id = $1`, tenant).Scan(&rowID); err != nil {
		t.Fatalf("row id: %v", err)
	}
	if err := db.ExecErr(`UPDATE hr_case_revision SET state = 'OPEN' WHERE tenant_id = $1 AND row_id = $2`, tenant, rowID); err == nil {
		t.Fatal("UPDATE append-only row succeeded")
	}
	if err := db.ExecErr(`DELETE FROM hr_case_revision WHERE tenant_id = $1 AND row_id = $2`, tenant, rowID); err == nil {
		t.Fatal("DELETE append-only row succeeded")
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM hr_case_revision WHERE tenant_id = $1`, tenant).Scan(&count); err != nil {
		t.Fatalf("count after mutation attempts: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows after mutation attempts = %d, want 1", count)
	}
}
