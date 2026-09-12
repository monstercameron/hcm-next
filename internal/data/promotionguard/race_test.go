package promotionguard_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// TestTodo_PROMOUX_002_Race runs genuinely concurrent admissions for the same
// worker and effective date, each in its own goroutine and its own real
// PostgreSQL transaction against the embedded pgtest server, and proves
// exactly one wins.
//
// This is deliberately not "SELECT to check, then INSERT": if it were, this
// test would be flaky at best (every goroutine's SELECT plausibly observes
// "no conflict" before any of them commits) and would eventually demonstrate
// the exact defect PROMOUX-002 exists to close -- two admitted promotions for
// one worker's one effective window. What actually decides the winner here
// is migrations/00286's partial unique index,
// promotion_active_intent_guard_one_active_window, on
// (tenant_id, worker_ref, effective_date) WHERE status = 'ACTIVE': PostgreSQL
// evaluates it as part of each transaction's own commit, so of N concurrent
// INSERTs only one can ever land, and this test asserts exactly that count
// rather than merely "no error".
func TestTodo_PROMOUX_002_Race(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "promoux002-race", "PROMOUX-002 race tenant")

	const worker = "EMPLOYMENT:promoux002-race-worker"
	const effectiveDate = "2027-11-01"
	const concurrency = 12

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		admitted  int
		conflicts int
		otherErrs []error
	)
	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()
			<-start
			// Each goroutine holds a genuinely independent connection and
			// transaction (internal/data/pgtest.DB.Conn is documented as not
			// safe for concurrent use; NewConn is the escape hatch this
			// package's own doc names for concurrency tests), so the
			// exclusion this test proves comes from PostgreSQL serializing
			// the commits, not from goroutines taking turns on one session.
			conn := db.NewConn(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				mu.Lock()
				otherErrs = append(otherErrs, err)
				mu.Unlock()
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
				mu.Lock()
				otherErrs = append(otherErrs, err)
				mu.Unlock()
				return
			}
			// Every goroutine presents its own distinct idempotency key: this
			// proves exclusion between genuinely different concurrent
			// requests, not deduplication of one retried request (that is
			// TestTodo_PROMOUX_002's and TestTodo_PROMOUX_002_Recovery's claim).
			key := uuid.New().String()
			_, admitErr := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, effectiveDate, key)
			if admitErr == nil {
				if commitErr := tx.Commit(ctx); commitErr != nil {
					mu.Lock()
					otherErrs = append(otherErrs, commitErr)
					mu.Unlock()
					return
				}
				mu.Lock()
				admitted++
				mu.Unlock()
				return
			}
			mu.Lock()
			if errors.Is(admitErr, promotionguard.ErrActiveConflict) {
				conflicts++
			} else {
				otherErrs = append(otherErrs, admitErr)
			}
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()

	for _, err := range otherErrs {
		t.Errorf("unexpected error from a concurrent Admit: %v", err)
	}
	if admitted != 1 {
		t.Fatalf("admitted = %d of %d concurrent callers, want exactly 1", admitted, concurrency)
	}
	if conflicts != concurrency-1 {
		t.Fatalf("conflicts = %d, want %d (every caller but the winner)", conflicts, concurrency-1)
	}

	readTx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, readTx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	var rowCount int
	if err := readTx.QueryRow(ctx,
		`SELECT count(*) FROM promotion_active_intent_guard WHERE tenant_id=$1 AND worker_ref=$2 AND effective_date=$3 AND status='ACTIVE'`,
		tenantID, worker, effectiveDate,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count durable rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("durable ACTIVE rows for the contested window = %d, want exactly 1 -- the constraint, not just the in-process count, must have held", rowCount)
	}
}
