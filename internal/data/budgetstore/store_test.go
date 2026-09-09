package budgetstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/budgetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var budgetNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func insertBudgetTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
        VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func budgetAppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func reservationFixture(t *testing.T, tenant uuid.UUID) (budget.CompensationReservationRequest, budget.CompensationBudgetAuthority) {
	t.Helper()
	amount, err := values.NewDecimal("60.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	available, err := values.NewDecimal("100.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	authorityDigest := canonicalbytes.Digest([]byte("authority"))
	return budget.CompensationReservationRequest{
			TenantID: tenant.String(), BudgetID: "pool-1", ProposalDigest: canonicalbytes.Digest([]byte("proposal")),
			Amount: amount, Currency: "USD", AuthorityDigest: authorityDigest,
			IdempotencyKey: "reservation-1", ExpiresAt: budgetNow.Add(time.Hour),
		}, budget.CompensationBudgetAuthority{
			BudgetID: "pool-1", Currency: "USD", Available: available, AuthorityDigest: authorityDigest,
		}
}

func newBudgetStore(t *testing.T, db *pgtest.DB, tenant uuid.UUID) *budgetstore.Store {
	t.Helper()
	return budgetstore.New(budgetAppConn(t, db), tenant)
}

func TestTodo_PERSIST_BUDGET_001(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertBudgetTenant(t, db, "persist-budget-primary")
	store := newBudgetStore(t, db, tenant)
	request, authority := reservationFixture(t, tenant)

	hold, err := store.Reserve(request, authority, budgetNow)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if hold.State != budget.Held || hold.Fence != 1 {
		t.Fatalf("hold = %#v, want HELD at fence 1", hold)
	}
	replay, err := store.Reserve(request, authority, budgetNow)
	if err != nil || replay.ID != hold.ID {
		t.Fatalf("idempotent replay = %#v, %v", replay, err)
	}

	committed, err := store.Commit(hold.ID, hold.Fence, budgetNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if committed.State != budget.Committed || committed.Fence != 2 {
		t.Fatalf("committed = %#v, want COMMITTED at fence 2", committed)
	}
	if _, err := store.Commit(committed.ID, committed.Fence, budgetNow.Add(time.Minute)); err != nil {
		t.Fatalf("idempotent terminal commit: %v", err)
	}
	evidence, ok := store.Evidence(hold.ID)
	if !ok || evidence.State != budget.Committed || len(evidence.Events) != 2 {
		t.Fatalf("evidence = %#v, found=%v", evidence, ok)
	}
	if evidence.Events[0].Sequence != 1 || evidence.Events[1].Sequence != 2 {
		t.Fatalf("event sequence = %#v, want [1 2]", evidence.Events)
	}
}

func TestTodo_PERSIST_BUDGET_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertBudgetTenant(t, db, "persist-budget-fault")
	store := newBudgetStore(t, db, tenant)
	request, authority := reservationFixture(t, tenant)
	hold, err := store.Reserve(request, authority, budgetNow)
	if err != nil {
		t.Fatal(err)
	}

	duplicate := request
	duplicate.ProposalDigest = canonicalbytes.Digest([]byte("different-proposal"))
	if _, err := store.Reserve(duplicate, authority, budgetNow); budgetstore.CodeOf(err) != budgetstore.CodeDuplicate || !errors.Is(err, budget.ErrReservationConflict) {
		t.Fatalf("duplicate idempotency refusal = %v, code=%q", err, budgetstore.CodeOf(err))
	}
	if _, err := store.Release(hold.ID, hold.Fence+1, budgetNow); budgetstore.CodeOf(err) != budgetstore.CodeStaleFence || !errors.Is(err, budget.ErrStaleFence) {
		t.Fatalf("stale fence refusal = %v, code=%q", err, budgetstore.CodeOf(err))
	}

	var sequences []int64
	rows, err := db.Conn.Query(context.Background(), `SELECT sequence FROM reservation_event WHERE reservation_id=$1 ORDER BY sequence`, uuid.MustParse(hold.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sequence int64
		if err := rows.Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		sequences = append(sequences, sequence)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(sequences) != 1 || sequences[0] != 1 {
		t.Fatalf("failed writes changed event sequence: %v", sequences)
	}
}

func TestTodo_PERSIST_BUDGET_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertBudgetTenant(t, db, "persist-budget-integration")
	store := newBudgetStore(t, db, tenant)
	request, authority := reservationFixture(t, tenant)
	hold, err := store.Reserve(request, authority, budgetNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkAmbiguous(hold.ID, hold.Fence, budgetNow.Add(time.Minute)); !errors.Is(err, budget.ErrExternalAmbiguous) {
		t.Fatalf("mark ambiguous = %v", err)
	}
	ambiguous, ok := store.Get(hold.ID)
	if !ok || ambiguous.State != budget.ReconciliationRequired || ambiguous.Fence != 2 {
		t.Fatalf("ambiguous reservation = %#v, found=%v", ambiguous, ok)
	}
	released, err := store.Reconcile(hold.ID, ambiguous.Fence, budget.Released, budgetNow.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if released.State != budget.Released || len(store.Events(hold.ID)) != 3 {
		t.Fatalf("released = %#v, events=%d", released, len(store.Events(hold.ID)))
	}
}

func TestTodo_PERSIST_BUDGET_001_Security(t *testing.T) {
	db := pgtest.New(t)
	first := insertBudgetTenant(t, db, "persist-budget-security-a")
	second := insertBudgetTenant(t, db, "persist-budget-security-b")
	firstStore := newBudgetStore(t, db, first)
	request, authority := reservationFixture(t, first)
	hold, err := firstStore.Reserve(request, authority, budgetNow)
	if err != nil {
		t.Fatal(err)
	}
	secondStore := newBudgetStore(t, db, second)
	if _, ok := secondStore.Get(hold.ID); ok {
		t.Fatal("foreign tenant read a reservation")
	}
	foreignRequest := request
	foreignRequest.TenantID = first.String()
	if _, err := secondStore.Reserve(foreignRequest, authority, budgetNow); budgetstore.CodeOf(err) != budgetstore.CodeTenant {
		t.Fatalf("cross-tenant reserve = %v, code=%q", err, budgetstore.CodeOf(err))
	}
	var visible int
	secondConn := budgetAppConn(t, db)
	tx, err := secondConn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, second); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM compensation_reservation`).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("foreign tenant saw %d rows", visible)
	}
}

func TestTodo_PERSIST_BUDGET_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertBudgetTenant(t, db, "persist-budget-recovery")
	request, authority := reservationFixture(t, tenant)
	firstConn := budgetAppConn(t, db)
	firstStore := budgetstore.New(firstConn, tenant)
	hold, err := firstStore.Reserve(request, authority, budgetNow)
	if err != nil {
		t.Fatal(err)
	}
	freshConn := db.NewConn(t)
	if _, err := freshConn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	freshStore := budgetstore.New(freshConn, tenant)
	loaded, ok := freshStore.Get(hold.ID)
	if !ok || loaded.ID != hold.ID || loaded.Request.IdempotencyKey != request.IdempotencyKey {
		t.Fatalf("fresh connection loaded = %#v, found=%v", loaded, ok)
	}
	evidence, ok := freshStore.Evidence(hold.ID)
	if !ok || len(evidence.Events) != 1 || evidence.Events[0].Sequence != 1 {
		t.Fatalf("fresh connection evidence = %#v, found=%v", evidence, ok)
	}
}

func TestTodo_PERSIST_BUDGET_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertBudgetTenant(t, db, "persist-budget-mutation")
	store := newBudgetStore(t, db, tenant)
	request, authority := reservationFixture(t, tenant)
	hold, err := store.Reserve(request, authority, budgetNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE reservation_event SET to_state='RELEASED' WHERE tenant_id=$1 AND reservation_id=$2`, tenant, uuid.MustParse(hold.ID)); err == nil {
		t.Fatal("append-only reservation event accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM reservation_event WHERE tenant_id=$1 AND reservation_id=$2`, tenant, uuid.MustParse(hold.ID)); err == nil {
		t.Fatal("append-only reservation event accepted DELETE")
	}
	if err := db.ExecErr(`INSERT INTO reservation_event (tenant_id,row_id,reservation_id,sequence,from_state,to_state,fence,"at")
        VALUES ($1,$2,$3,3,'HELD','RELEASED',2,$4)`, tenant, uuid.New(), uuid.MustParse(hold.ID), budgetNow.Add(time.Minute)); err == nil {
		t.Fatal("reservation event accepted a sequence gap")
	}
	if released, err := store.Release(hold.ID, hold.Fence, budgetNow.Add(2*time.Minute)); err != nil || released.State != budget.Released {
		t.Fatalf("valid transition after rejected mutation = %#v, %v", released, err)
	}
}
