package admissionstore

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/operations/admission"
	"strings"
	"sync"
	"testing"
	"time"
)

type nilBeginner struct{}

func (*nilBeginner) Begin(context.Context) (dbport.Tx, error) { panic("typed nil database used") }

func TestMain(m *testing.M) { pgtest.RunMain(m) }
func fixture(t *testing.T) (*Store, Budget, *pgtest.DB) {
	t.Helper()
	db := pgtest.New(t)
	s := New(db.Conn)
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	b := Budget{BudgetID: uuid.New(), TenantID: uuid.New(), Service: "svc", Dependency: "db", LogicalOperationID: "op", OperationKind: "write", PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour), ExpiresAt: now.Add(time.Hour), Allowed: 1, Version: "v1", Owner: "ops", Retryable: []admission.FailureClass{admission.FailureTransient}}
	if err := s.CreateBudget(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	return s, b, db
}
func TestTodo_ADMISSION_002_Integration(t *testing.T) {
	s, b, _ := fixture(t)
	a := Attempt{BudgetID: b.BudgetID, AttemptID: uuid.New(), TenantID: b.TenantID, Service: b.Service, Dependency: b.Dependency, LogicalOperationID: "op", OperationKind: b.OperationKind, Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}
	r, err := s.Consume(context.Background(), a, b.PeriodStart.Add(time.Hour))
	if err != nil || r.Remaining != 0 {
		t.Fatalf("receipt=%+v err=%v", r, err)
	}
	again, err := s.Consume(context.Background(), a, b.PeriodStart.Add(time.Hour))
	if err != nil || again.Digest != r.Digest {
		t.Fatalf("replay=%+v/%v err=%v", again, r, err)
	}
}
func TestTodo_ADMISSION_002_Race(t *testing.T) {
	_, b, db := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s1, s2 := New(db.NewConn(t)), New(db.NewConn(t))
	attemptID := uuid.New()
	var wg sync.WaitGroup
	results := make(chan Receipt, 2)
	errs := make(chan error, 2)
	for i, s := range []*Store{s1, s2} {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			a := Attempt{BudgetID: b.BudgetID, AttemptID: attemptID, TenantID: b.TenantID, Service: b.Service, Dependency: b.Dependency, LogicalOperationID: b.LogicalOperationID, OperationKind: b.OperationKind, Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}
			r, err := s.Consume(ctx, a, b.PeriodStart.Add(time.Hour))
			if err != nil {
				errs <- err
			} else {
				results <- r
			}
		}(i, s)
	}
	wg.Wait()
	close(results)
	close(errs)
	if len(results) != 2 || len(errs) != 0 {
		t.Fatalf("same-attempt results=%d errors=%d", len(results), len(errs))
	}
	var first Receipt
	for r := range results {
		if first.ReceiptID == uuid.Nil {
			first = r
		} else if r.Digest != first.Digest {
			t.Fatalf("replay digests differ")
		}
	}
}

func TestTodo_ADMISSION_002_DistinctAttemptIDsWithSameOrdinalCompeteForFinalToken(t *testing.T) {
	_, b, db := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stores := []*Store{New(db.NewConn(t)), New(db.NewConn(t))}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, store := range stores {
		wg.Add(1)
		go func(attemptID uuid.UUID, store *Store) {
			defer wg.Done()
			_, err := store.Consume(ctx, Attempt{BudgetID: b.BudgetID, AttemptID: attemptID, TenantID: b.TenantID, Service: b.Service, Dependency: b.Dependency, LogicalOperationID: b.LogicalOperationID, OperationKind: b.OperationKind, Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}, b.PeriodStart.Add(time.Hour))
			errs <- err
		}(uuid.New(), store)
	}
	wg.Wait()
	close(errs)
	allowed, exhausted := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			allowed++
		case errors.Is(err, ErrExhausted):
			exhausted++
		default:
			t.Fatalf("unexpected competing consume error: %v", err)
		}
	}
	if allowed != 1 || exhausted != 1 {
		t.Fatalf("allowed/exhausted = %d/%d, want 1/1", allowed, exhausted)
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, b.TenantID); err != nil {
		t.Fatal(err)
	}
	var consumed int64
	if err := tx.QueryRow(ctx, `SELECT consumed FROM admission_retry_budget WHERE tenant_id=$1 AND budget_id=$2`, b.TenantID, b.BudgetID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != 1 {
		t.Fatalf("durable consumed = %d, want 1", consumed)
	}
}
func TestTodo_ADMISSION_002_Security(t *testing.T) {
	s, b, _ := fixture(t)
	a := Attempt{BudgetID: b.BudgetID, AttemptID: uuid.New(), TenantID: uuid.New(), Service: b.Service, Dependency: b.Dependency, LogicalOperationID: b.LogicalOperationID, OperationKind: b.OperationKind, Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}
	if _, err := s.Consume(context.Background(), a, b.PeriodStart.Add(time.Hour)); err == nil {
		t.Fatal("cross-tenant budget consumed")
	}
}
func TestTodo_ADMISSION_002_Fault(t *testing.T) {
	s, b, _ := fixture(t)
	a := Attempt{BudgetID: b.BudgetID, AttemptID: uuid.New(), TenantID: b.TenantID, Service: b.Service, Dependency: b.Dependency, LogicalOperationID: b.LogicalOperationID, OperationKind: b.OperationKind, Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}
	if _, err := s.Consume(context.Background(), a, b.PeriodEnd); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired err=%v", err)
	}
}
func TestTodo_ADMISSION_002_Mutation(t *testing.T) {
	s, b, _ := fixture(t)
	a := Attempt{BudgetID: b.BudgetID, AttemptID: uuid.New(), TenantID: b.TenantID, Service: b.Service, Dependency: b.Dependency, LogicalOperationID: b.LogicalOperationID, OperationKind: "other", Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}
	if _, err := s.Consume(context.Background(), a, b.PeriodStart.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("scope mismatch err=%v", err)
	}
}

func TestTodo_ADMISSION_002_ExactReplayBindsVersionAndFailure(t *testing.T) {
	s, b, _ := fixture(t)
	now := b.PeriodStart.Add(time.Hour)
	a := Attempt{BudgetID: b.BudgetID, AttemptID: uuid.New(), TenantID: b.TenantID, Service: b.Service, Dependency: b.Dependency, LogicalOperationID: b.LogicalOperationID, OperationKind: b.OperationKind, Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}
	if _, err := s.Consume(context.Background(), a, now); err != nil {
		t.Fatal(err)
	}
	changedFailure := a
	changedFailure.Failure = admission.FailureTimeout
	if _, err := s.Consume(context.Background(), changedFailure, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed failure replay error = %v", err)
	}
	changedVersion := a
	changedVersion.Version = "v2"
	if _, err := s.Consume(context.Background(), changedVersion, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("changed version error = %v", err)
	}
}

func TestReceiptDigestFramesAllScopeFields(t *testing.T) {
	base := Receipt{ReceiptID: uuid.New(), BudgetID: uuid.New(), AttemptID: uuid.New(), TenantID: uuid.New(), Service: "a|b", Dependency: "c", LogicalOperationID: "logical", OperationKind: "write", BudgetVersion: "v1", Attempt: 1, Failure: admission.FailureTransient, Consumed: 1, Remaining: 2}
	left, err := digestReceipt(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Service, base.Dependency = "a", "b|c"
	right, err := digestReceipt(base)
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("distinct framed scope fields produced the same receipt digest")
	}
}

func TestInvalidStoreAndBudgetAreRejected(t *testing.T) {
	if err := (*Store)(nil).CreateBudget(context.Background(), Budget{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store error = %v", err)
	}
	if _, err := (*Store)(nil).Consume(context.Background(), Attempt{}, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store consume error = %v", err)
	}
	if err := New((*nilBeginner)(nil)).CreateBudget(context.Background(), Budget{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("typed nil database error = %v", err)
	}
	invalid := Budget{BudgetID: uuid.New(), TenantID: uuid.New(), Service: "svc", Dependency: "db", LogicalOperationID: "op", OperationKind: "write", PeriodStart: time.Now(), PeriodEnd: time.Now().Add(time.Hour), ExpiresAt: time.Now().Add(time.Hour), Version: "v1", Owner: "ops", Retryable: []admission.FailureClass{admission.FailureTransient, admission.FailureTransient}}
	if err := New(nil).CreateBudget(context.Background(), invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate retryable error = %v", err)
	}
}

func TestMigrationDownRefusesToEraseBudgetEvidence(t *testing.T) {
	s, b, db := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := Attempt{BudgetID: b.BudgetID, AttemptID: uuid.New(), TenantID: b.TenantID, Service: b.Service, Dependency: b.Dependency, LogicalOperationID: b.LogicalOperationID, OperationKind: b.OperationKind, Version: b.Version, Attempt: 1, Failure: admission.FailureTransient}
	want, err := s.Consume(ctx, a, b.PeriodStart.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, downErr := db.Provider(t).Down(ctx)
	if downErr == nil || !strings.Contains(downErr.Error(), "SQLSTATE P0001") || !strings.Contains(downErr.Error(), "00280 is irreversible: retry receipts and consumed budget counters are durable evidence") {
		t.Fatalf("migration down error = %v, want exact 00280 P0001 refusal", downErr)
	}
	got, err := s.Consume(ctx, a, b.PeriodEnd)
	if err != nil || got.ReceiptID != want.ReceiptID || got.Digest != want.Digest {
		t.Fatalf("receipt after refused down = %+v, %v; want identical %+v", got, err, want)
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, b.TenantID); err != nil {
		t.Fatal(err)
	}
	var consumed int64
	if err := tx.QueryRow(ctx, `SELECT consumed FROM admission_retry_budget WHERE tenant_id=$1 AND budget_id=$2`, b.TenantID, b.BudgetID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != 1 {
		t.Fatalf("consumed after refused down = %d, want 1", consumed)
	}
}
