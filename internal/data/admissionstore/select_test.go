package admissionstore

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"testing"
	"time"
)

type selectProbeDB struct {
	inner    DB
	readOnly string
}

func (p *selectProbeDB) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := p.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &selectProbeTx{Tx: tx, owner: p}, nil
}

type selectProbeTx struct {
	dbport.Tx
	owner *selectProbeDB
}

func (p *selectProbeTx) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	if err := p.Tx.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&p.owner.readOnly); err != nil {
		return nil, err
	}
	return p.Tx.Query(ctx, sql, args...)
}

type selectErrorDB struct{ err error }

func (d selectErrorDB) Begin(context.Context) (dbport.Tx, error) { return nil, d.err }

func TestTodo_ADMISSION_002_SelectActiveBudget(t *testing.T) {
	s, b, db := fixture(t)
	now := b.PeriodStart.Add(time.Hour)
	got, err := s.SelectActiveBudget(context.Background(), b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, now)
	if err != nil || got.BudgetID != b.BudgetID || got.Version != b.Version {
		t.Fatalf("selected=%+v err=%v", got, err)
	}
	fresh := New(db.NewConn(t))
	if _, err := fresh.SelectActiveBudget(context.Background(), b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, "wrong", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong version err=%v", err)
	}
	if _, err := fresh.SelectActiveBudget(context.Background(), uuid.NewString(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong tenant err=%v", err)
	}
}

func TestTodo_ADMISSION_002_SelectActiveBudget_Conflict(t *testing.T) {
	s, b, _ := fixture(t)
	b.BudgetID = uuid.New()
	b.PeriodStart = b.PeriodStart.Add(10 * time.Minute)
	if err := s.CreateBudget(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	_, err := s.SelectActiveBudget(context.Background(), b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, b.PeriodStart.Add(20*time.Minute))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active selection err=%v", err)
	}
}

func TestTodo_ADMISSION_002_SelectActiveBudgetRejectsWrongScopeAndTime(t *testing.T) {
	s, b, _ := fixture(t)
	now := b.PeriodStart.Add(time.Hour)
	tests := []struct {
		name, tenant, service, dependency, logical, kind, version string
		at                                                        time.Time
	}{
		{"tenant", uuid.NewString(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, now},
		{"service", b.TenantID.String(), "other", b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, now},
		{"dependency", b.TenantID.String(), b.Service, "other", b.LogicalOperationID, b.OperationKind, b.Version, now},
		{"logical", b.TenantID.String(), b.Service, b.Dependency, "other", b.OperationKind, b.Version, now},
		{"kind", b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, "other", b.Version, now},
		{"version", b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, "other", now},
		{"future", b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, b.PeriodStart.Add(-time.Nanosecond)},
		{"ended", b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, b.PeriodEnd},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.SelectActiveBudget(context.Background(), tc.tenant, tc.service, tc.dependency, tc.logical, tc.kind, tc.version, tc.at)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestTodo_ADMISSION_002_SelectActiveBudgetRejectsRevoked(t *testing.T) {
	s, b, db := fixture(t)
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, b.TenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE admission_retry_budget SET status='REVOKED' WHERE tenant_id=$1 AND budget_id=$2`, b.TenantID, b.BudgetID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = s.SelectActiveBudget(ctx, b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, b.PeriodStart.Add(time.Hour))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked error = %v", err)
	}
}

func TestTodo_ADMISSION_002_SelectActiveBudgetIsReadOnlyOnPostgres(t *testing.T) {
	_, b, db := fixture(t)
	probe := &selectProbeDB{inner: db.NewConn(t)}
	_, err := New(probe).SelectActiveBudget(context.Background(), b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, b.PeriodStart.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if probe.readOnly != "on" {
		t.Fatalf("transaction_read_only = %q, want on", probe.readOnly)
	}
}

func TestTodo_ADMISSION_002_SelectActiveBudgetInputAndStorageFailures(t *testing.T) {
	args := func(s *Store, ctx context.Context) error {
		_, err := s.SelectActiveBudget(ctx, uuid.NewString(), "svc", "dep", "logical", "kind", "v1", time.Now())
		return err
	}
	if err := args((*Store)(nil), context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store = %v", err)
	}
	if err := args(New((*nilBeginner)(nil)), context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("typed nil DB = %v", err)
	}
	want := errors.New("begin failed")
	if err := args(New(selectErrorDB{err: want}), context.Background()); !errors.Is(err, want) {
		t.Fatalf("storage error = %v", err)
	}
	s, b, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.SelectActiveBudget(ctx, b.TenantID.String(), b.Service, b.Dependency, b.LogicalOperationID, b.OperationKind, b.Version, b.PeriodStart.Add(time.Hour))
	if err == nil || (!errors.Is(err, context.Canceled) && !strings.Contains(strings.ToLower(err.Error()), "cancel")) {
		t.Fatalf("canceled context error = %v", err)
	}
}
