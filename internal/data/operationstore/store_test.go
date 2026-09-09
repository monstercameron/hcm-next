package operationstore

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func operationTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-operation-test', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, key, "Tenant "+key)
	return id
}

func operationFixture(tenant uuid.UUID, id string, state State) Record {
	return Record{
		OperationID: id,
		TenantID:    tenant.String(),
		Owner:       "principal:operator",
		RequestType: "promotion.propose",
		State:       state,
		MetadataRef: "operation-metadata:" + id,
		CreatedAt:   time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

func TestTodo_EP_OPS_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := operationTenant(t, db, "operation-integration")
	conn := db.NewConn(t)
	store := New(conn)
	want := operationFixture(tenant, "op-integration", StateSucceeded)
	want.ResultBytes = []byte("typed-result-wire")
	if err := store.Put(t.Context(), want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(t.Context(), tenant.String(), want.OperationID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.OperationID != want.OperationID || got.TenantID != tenant.String() || got.Owner != want.Owner || got.State != want.State || string(got.ResultBytes) != string(want.ResultBytes) {
		t.Fatalf("Get = %+v, want identity and state from %+v", got, want)
	}
}

func TestTodo_EP_OPS_001_Security(t *testing.T) {
	db := pgtest.New(t)
	tenant := operationTenant(t, db, "operation-security")
	other := operationTenant(t, db, "operation-security-other")
	conn := db.NewConn(t)
	store := New(conn)
	fixture := operationFixture(tenant, "op-private", StateRunning)
	if err := store.Put(t.Context(), fixture); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := store.Get(t.Context(), other.String(), fixture.OperationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant Get = %v, want ErrNotFound", err)
	}
	appConn := db.NewConn(t)
	if _, err := appConn.Exec(t.Context(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	var visible int
	if err := appConn.QueryRow(t.Context(), `SELECT count(*) FROM operation WHERE tenant_id = $1`, tenant).Scan(&visible); err != nil {
		t.Fatalf("unscoped operation count: %v", err)
	}
	if visible != 0 {
		t.Fatalf("unscoped app-role count = %d, want RLS to hide it", visible)
	}
}

func TestTodo_EP_OPS_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := operationTenant(t, db, "operation-recovery")
	store := New(db.NewConn(t))
	fixture := operationFixture(tenant, "op-cancel", StateRunning)
	if err := store.Put(t.Context(), fixture); err != nil {
		t.Fatalf("Put: %v", err)
	}
	first, err := store.Cancel(t.Context(), tenant.String(), fixture.OperationID, "cancel-1", "operator-request")
	if err != nil {
		t.Fatalf("first Cancel: %v", err)
	}
	second, err := store.Cancel(t.Context(), tenant.String(), fixture.OperationID, "cancel-1", "operator-request")
	if err != nil {
		t.Fatalf("replayed Cancel: %v", err)
	}
	if first.State != StateCancellationRequested || second.State != first.State {
		t.Fatalf("cancel states = %s then %s, want idempotent cancellation request", first.State, second.State)
	}
	finished, err := store.Transition(t.Context(), tenant.String(), fixture.OperationID, 2, StateCancellationRequested, StateCancelled)
	if err != nil {
		t.Fatalf("fenced Transition: %v", err)
	}
	if finished.State != StateCancelled {
		t.Fatalf("finished state = %s, want CANCELLED", finished.State)
	}
	if _, err := store.Transition(t.Context(), tenant.String(), fixture.OperationID, 2, StateCancelled, StateRunning); !errors.Is(err, ErrFenced) {
		t.Fatalf("stale Transition = %v, want ErrFenced", err)
	}
}
