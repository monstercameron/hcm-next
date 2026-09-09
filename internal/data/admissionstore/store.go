package admissionstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

type DB interface{ dbport.Beginner }
type Budget struct {
	BudgetID                          uuid.UUID
	TenantID                          uuid.UUID
	Service, Dependency               string
	LogicalOperationID                string
	OperationKind                     string
	PeriodStart, PeriodEnd, ExpiresAt time.Time
	Allowed                           int64
	Refunded                          int64
	Retryable                         []admission.FailureClass
	Version, Owner                    string
}
type Attempt struct {
	BudgetID, AttemptID                                             uuid.UUID
	TenantID                                                        uuid.UUID
	Service, Dependency, LogicalOperationID, OperationKind, Version string
	Attempt                                                         int
	Failure                                                         admission.FailureClass
}
type Receipt struct {
	ReceiptID, BudgetID, AttemptID                                        uuid.UUID
	TenantID                                                              uuid.UUID
	Service, Dependency, LogicalOperationID, OperationKind, BudgetVersion string
	Attempt                                                               int
	Failure                                                               admission.FailureClass
	Consumed, Remaining                                                   int64
	Digest                                                                string
}

var (
	ErrInvalid       = errors.New("admissionstore: invalid input")
	ErrNotFound      = errors.New("admissionstore: budget not found")
	ErrConflict      = errors.New("admissionstore: conflicting replay")
	ErrExhausted     = errors.New("admissionstore: retry budget exhausted")
	ErrCommitUnknown = errors.New("admissionstore: transaction outcome is unknown")
)

type Store struct{ db DB }

func New(db DB) *Store { return &Store{db: db} }

func (s *Store) CreateBudget(ctx context.Context, b Budget) error {
	if s == nil || nilDB(s.db) || b.BudgetID == uuid.Nil || b.TenantID == uuid.Nil || strings.TrimSpace(b.Service) == "" || strings.TrimSpace(b.Dependency) == "" || strings.TrimSpace(b.LogicalOperationID) == "" || strings.TrimSpace(b.OperationKind) == "" || b.Allowed < 0 || b.Refunded < 0 || b.Allowed > math.MaxInt64-b.Refunded || b.PeriodStart.IsZero() || b.PeriodEnd.IsZero() || !b.PeriodEnd.After(b.PeriodStart) || b.ExpiresAt.IsZero() || !b.ExpiresAt.After(b.PeriodStart) || b.ExpiresAt.After(b.PeriodEnd) || strings.TrimSpace(b.Version) == "" || strings.TrimSpace(b.Owner) == "" || !validRetryable(b.Retryable) {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, b.TenantID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO admission_retry_budget (budget_id,tenant_id,service,dependency,logical_operation_id,operation_kind,period_start,period_end,expires_at,status,allowed,refunded,retryable,version,owner) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'ACTIVE',$10,$11,$12,$13,$14)`, b.BudgetID, b.TenantID, b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.PeriodStart, b.PeriodEnd, b.ExpiresAt, b.Allowed, b.Refunded, classes(b.Retryable), b.Version, b.Owner)
	if err != nil {
		return fmt.Errorf("admissionstore: create: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrCommitUnknown, err)
	}
	return nil
}

func (s *Store) Consume(ctx context.Context, a Attempt, now time.Time) (Receipt, error) {
	if s == nil || nilDB(s.db) || a.BudgetID == uuid.Nil || a.AttemptID == uuid.Nil || a.TenantID == uuid.Nil || strings.TrimSpace(a.Service) == "" || strings.TrimSpace(a.Dependency) == "" || strings.TrimSpace(a.LogicalOperationID) == "" || strings.TrimSpace(a.OperationKind) == "" || strings.TrimSpace(a.Version) == "" || a.Attempt <= 0 {
		return Receipt{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, a.TenantID); err != nil {
		return Receipt{}, err
	}
	var r Receipt
	var digest string
	var tenant uuid.UUID
	var service, dependency, kind, status, logical string
	var version string
	var allowed, consumed, refunded int64
	var expires, periodStart, periodEnd time.Time
	var retryable []string
	err = tx.QueryRow(ctx, `SELECT tenant_id,service,dependency,logical_operation_id,operation_kind,status,allowed,consumed,refunded,expires_at,period_start,period_end,retryable,version FROM admission_retry_budget WHERE tenant_id=$1 AND budget_id=$2 FOR UPDATE`, a.TenantID, a.BudgetID).Scan(&tenant, &service, &dependency, &logical, &kind, &status, &allowed, &consumed, &refunded, &expires, &periodStart, &periodEnd, &retryable, &version)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Receipt{}, ErrNotFound
		}
		return Receipt{}, err
	}
	if service != a.Service || dependency != a.Dependency || kind != a.OperationKind || logical != a.LogicalOperationID || version != a.Version {
		return Receipt{}, ErrNotFound
	}
	// Replays are checked after locking the budget so same-attempt callers serialize.
	err = tx.QueryRow(ctx, `SELECT receipt_id,budget_id,attempt_id,tenant_id,service,dependency,logical_operation_id,operation_kind,budget_version,attempt,failure_class,consumed,remaining,receipt_digest FROM admission_retry_receipt WHERE tenant_id=$1 AND budget_id=$2 AND attempt_id=$3`, a.TenantID, a.BudgetID, a.AttemptID).Scan(&r.ReceiptID, &r.BudgetID, &r.AttemptID, &r.TenantID, &r.Service, &r.Dependency, &r.LogicalOperationID, &r.OperationKind, &r.BudgetVersion, &r.Attempt, &r.Failure, &r.Consumed, &r.Remaining, &digest)
	if err == nil {
		if r.LogicalOperationID != a.LogicalOperationID || r.Service != a.Service || r.Dependency != a.Dependency || r.OperationKind != a.OperationKind || r.BudgetVersion != a.Version || r.Attempt != a.Attempt || r.Failure != a.Failure {
			return Receipt{}, ErrConflict
		}
		expected, digestErr := digestReceipt(r)
		if digestErr != nil || expected != digest {
			return Receipt{}, ErrInvalid
		}
		r.Digest = digest
		return r, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return Receipt{}, err
	}
	if status != "ACTIVE" || !now.Before(expires) || now.Before(periodStart) || !now.Before(periodEnd) {
		return Receipt{}, ErrNotFound
	}
	if allowed > math.MaxInt64-refunded || consumed > allowed+refunded || allowed > int64(maxInt()) || consumed > int64(maxInt()) || refunded > int64(maxInt()) {
		return Receipt{}, ErrInvalid
	}
	budget := admission.RetryBudget{ID: a.BudgetID.String(), TenantID: a.TenantID.String(), Service: service, Dependency: dependency, LogicalOperationID: logical, OperationKind: kind, Allowed: int(allowed), Consumed: int(consumed), Refunded: int(refunded), Retryable: classesFailure(retryable), Version: version}
	attempt := admission.RetryAttempt{LogicalOperationID: a.LogicalOperationID, OperationKind: a.OperationKind, TenantID: a.TenantID.String(), Dependency: a.Dependency, Failure: a.Failure, Attempt: a.Attempt}
	decision, decErr := admission.ConsumeRetry(budget, attempt)
	if decErr != nil {
		return Receipt{}, decErr
	}
	if decision.Disposition != admission.RetryAllowed {
		return Receipt{}, ErrExhausted
	}
	remaining := int64(decision.Remaining)
	consumed++
	r = Receipt{ReceiptID: uuid.New(), BudgetID: a.BudgetID, AttemptID: a.AttemptID, TenantID: a.TenantID, Service: a.Service, Dependency: a.Dependency, LogicalOperationID: a.LogicalOperationID, OperationKind: a.OperationKind, BudgetVersion: a.Version, Attempt: a.Attempt, Failure: a.Failure, Consumed: 1, Remaining: remaining}
	r.Digest, err = digestReceipt(r)
	if err != nil {
		return Receipt{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE admission_retry_budget SET consumed=$3 WHERE tenant_id=$1 AND budget_id=$2`, a.TenantID, a.BudgetID, consumed); err != nil {
		return Receipt{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admission_retry_receipt (receipt_id,tenant_id,budget_id,attempt_id,service,dependency,logical_operation_id,operation_kind,budget_version,attempt,failure_class,receipt_digest,consumed,remaining) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, r.ReceiptID, r.TenantID, r.BudgetID, r.AttemptID, r.Service, r.Dependency, r.LogicalOperationID, r.OperationKind, r.BudgetVersion, r.Attempt, r.Failure, r.Digest, r.Consumed, r.Remaining); err != nil {
		return Receipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Receipt{}, fmt.Errorf("%w: %v", ErrCommitUnknown, err)
	}
	return r, nil
}
func classes(v []admission.FailureClass) []string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = string(x)
	}
	return out
}
func classesFailure(v []string) []admission.FailureClass {
	out := make([]admission.FailureClass, len(v))
	for i, x := range v {
		out[i] = admission.FailureClass(x)
	}
	return out
}
func maxInt() int { return int(^uint(0) >> 1) }
func nilDB(db DB) bool {
	if db == nil {
		return true
	}
	v := reflect.ValueOf(db)
	return (v.Kind() == reflect.Chan || v.Kind() == reflect.Func || v.Kind() == reflect.Interface || v.Kind() == reflect.Map || v.Kind() == reflect.Pointer || v.Kind() == reflect.Slice) && v.IsNil()
}
func validRetryable(v []admission.FailureClass) bool {
	seen := make(map[admission.FailureClass]bool, len(v))
	for _, x := range v {
		if seen[x] || (x != admission.FailureTransient && x != admission.FailureUnavailable && x != admission.FailureThrottled && x != admission.FailureTimeout) {
			return false
		}
		seen[x] = true
	}
	return true
}
func digestReceipt(r Receipt) (string, error) {
	return canonicalbytes.New("hcmnext.data.admissionstore.RetryReceipt", 1).
		String("receipt_id", r.ReceiptID.String()).String("tenant_id", r.TenantID.String()).String("budget_id", r.BudgetID.String()).String("attempt_id", r.AttemptID.String()).
		String("service", r.Service).String("dependency", r.Dependency).
		String("logical_operation_id", r.LogicalOperationID).String("operation_kind", r.OperationKind).
		String("budget_version", r.BudgetVersion).Int("attempt", int64(r.Attempt)).
		String("failure_class", string(r.Failure)).Int("consumed", r.Consumed).Int("remaining", r.Remaining).Digest()
}
