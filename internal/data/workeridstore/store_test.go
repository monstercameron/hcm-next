package workeridstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestStorePersistsPolicyWithCASAndReservesConcurrently(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-worker-id','Worker IDs','ACTIVE',$3)`, tenantID, "worker-id-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	tenant := values.TenantId("worker-id-test")
	policy := workerids.DefaultPolicy()
	policy.Prefix, policy.SequenceDigits, policy.StartAt, policy.NextSequence = "NW", 5, 98, 98
	policy.ExcludedRanges = "100-104"
	saved, err := store.Save(context.Background(), tenant, "org:north", "admin", policy)
	if err != nil || saved.Version != 1 {
		t.Fatalf("save: %+v err=%v", saved, err)
	}
	if _, err := store.Save(context.Background(), tenant, "org:north", "admin", policy); !errors.Is(err, workerids.ErrVersionConflict) {
		t.Fatalf("stale save err=%v", err)
	}

	const count = 24
	concurrentStores := make([]*Store, count)
	for i := range concurrentStores {
		concurrentStores[i] = New(db.NewConn(t), func(values.TenantId) uuid.UUID { return tenantID })
	}
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		workerStore := concurrentStores[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, reserveErr := workerStore.Reserve(context.Background(), tenant, "org:north", "hiring", workerids.FormatContext{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
			if reserveErr != nil {
				errs <- reserveErr
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Errorf("reserve: %v", err)
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Errorf("duplicate reservation %q", id)
		}
		seen[id] = true
		if id == "NW-00100" {
			t.Error("excluded sequence was issued")
		}
	}
	if len(seen) != count {
		t.Fatalf("reserved %d unique ids, want %d", len(seen), count)
	}
	loaded, err := store.Load(context.Background(), tenant, "org:north")
	if err != nil || loaded.IssuedCount != count {
		t.Fatalf("load: %+v err=%v", loaded, err)
	}
}

// TestReserveTxRollsBackWithItsCallerAndNeverBurnsTheNumber is the
// PROMOUX-001 follow-up regression: it proves ReserveTx's whole reason to
// exist. Reserve commits its reservation in its own transaction the instant
// it returns, so a caller that reserves through Reserve and then fails to
// insert the row that number was for (a separate, later transaction) is
// left with a permanently orphaned reservation -- worker_id_reservation is
// append-only, so nothing can ever release it, and demo-people-style
// re-seeding treats every one of those orphaned numbers as spent forever
// while the worker behind it was never created.
//
// This proves the fix: reserving and inserting inside ONE transaction means
// a mid-run failure (simulated here as a rollback right after reserving)
// takes the reservation down with it, and a genuine retry recovers the
// exact same number rather than skipping past it as already spent.
func TestReserveTxRollsBackWithItsCallerAndNeverBurnsTheNumber(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-worker-id-atomic','Worker IDs Atomic','ACTIVE',$3)`, tenantID, "worker-id-atomic", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	fc := workerids.FormatContext{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

	// The original, failed seeding run: reserve a number, then the caller's
	// transaction fails for an unrelated reason (here, simulated directly
	// as a rollback -- in journeyEngine.CreateWorker's real shape, this is
	// the worker-row insert failing after a successful reservation).
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	reserved, err := store.ReserveTx(ctx, tx, tenantID, "org:atomic", "hiring", fc)
	if err != nil {
		t.Fatalf("ReserveTx: %v", err)
	}
	if reserved == "" {
		t.Fatal("ReserveTx returned no number")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// The orphaned reservation must not have survived: this is the
	// database-observable proof the earlier "inserted:0, skipped:N" replay
	// failure traced back to.
	var count int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM worker_id_reservation WHERE tenant_id=$1 AND worker_number=$2`, tenantID, reserved).Scan(&count); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("reservation %q survived a rolled-back transaction: found %d row(s), want 0", reserved, count)
	}

	// The re-run: a fresh transaction that actually commits recovers the
	// SAME number the rolled-back attempt tried, proving nothing was
	// permanently spent, then the sequence advances normally afterward.
	tx2, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin retry: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx2, tenantID); err != nil {
		t.Fatalf("scope tenant retry: %v", err)
	}
	retried, err := store.ReserveTx(ctx, tx2, tenantID, "org:atomic", "hiring", fc)
	if err != nil {
		t.Fatalf("ReserveTx retry: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit retry: %v", err)
	}
	if retried != reserved {
		t.Fatalf("retry reserved %q, want the same number %q the rolled-back attempt never actually spent", retried, reserved)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM worker_id_reservation WHERE tenant_id=$1 AND worker_number=$2`, tenantID, retried).Scan(&count); err != nil {
		t.Fatalf("count after commit: %v", err)
	}
	if count != 1 {
		t.Fatalf("committed reservation %q recorded %d time(s), want exactly 1", retried, count)
	}

	// The sequence keeps moving forward from there -- the retry's success
	// consumed the number for real, so the NEXT reservation is a new one.
	tx3, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin next: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx3, tenantID); err != nil {
		t.Fatalf("scope tenant next: %v", err)
	}
	next, err := store.ReserveTx(ctx, tx3, tenantID, "org:atomic", "hiring", fc)
	if err != nil {
		t.Fatalf("ReserveTx next: %v", err)
	}
	if err := tx3.Commit(ctx); err != nil {
		t.Fatalf("commit next: %v", err)
	}
	if next == retried {
		t.Fatalf("next reservation %q reused an already-committed number", next)
	}
}
