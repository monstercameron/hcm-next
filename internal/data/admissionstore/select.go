package admissionstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// SelectActiveBudget returns the one persisted budget that governs the exact
// requested operation scope. It never synthesizes authority from caller
// input, and duplicate matches fail closed.
func (s *Store) SelectActiveBudget(ctx context.Context, tenant, service, dependency, logicalOperationID, operationKind, version string, now time.Time) (Budget, error) {
	tenantID, err := uuid.Parse(strings.TrimSpace(tenant))
	if err != nil || tenantID == uuid.Nil || strings.TrimSpace(service) == "" || strings.TrimSpace(dependency) == "" || strings.TrimSpace(logicalOperationID) == "" || strings.TrimSpace(operationKind) == "" || strings.TrimSpace(version) == "" || now.IsZero() {
		return Budget{}, ErrInvalid
	}
	if s == nil || nilDB(s.db) {
		return Budget{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Budget{}, err
	}
	defer tx.Rollback(ctx)
	// PostgreSQL only accepts transaction characteristics before the first
	// query. Establish read-only before setting the tenant-local RLS value.
	if _, err := tx.Exec(ctx, `SET TRANSACTION READ ONLY`); err != nil {
		return Budget{}, err
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return Budget{}, err
	}
	rows, err := tx.Query(ctx, `SELECT budget_id,tenant_id,service,dependency,logical_operation_id,operation_kind,period_start,period_end,expires_at,allowed,refunded,retryable,version,owner FROM admission_retry_budget WHERE tenant_id=$1 AND service=$2 AND dependency=$3 AND logical_operation_id=$4 AND operation_kind=$5 AND version=$6 AND status='ACTIVE' AND period_start <= $7 AND period_end > $7 AND expires_at > $7 LIMIT 2`, tenantID, service, dependency, logicalOperationID, operationKind, version, now)
	if err != nil {
		return Budget{}, err
	}
	defer rows.Close()
	var selected Budget
	count := 0
	for rows.Next() {
		var retryable []string
		var b Budget
		if err := rows.Scan(&b.BudgetID, &b.TenantID, &b.Service, &b.Dependency, &b.LogicalOperationID, &b.OperationKind, &b.PeriodStart, &b.PeriodEnd, &b.ExpiresAt, &b.Allowed, &b.Refunded, &retryable, &b.Version, &b.Owner); err != nil {
			return Budget{}, err
		}
		b.Retryable = classesFailure(retryable)
		if b.TenantID != tenantID || b.Service != service || b.Dependency != dependency || b.LogicalOperationID != logicalOperationID || b.OperationKind != operationKind || b.Version != version || b.BudgetID == uuid.Nil || strings.TrimSpace(b.Owner) == "" || b.Allowed < 0 || b.Refunded < 0 || !validRetryable(b.Retryable) || b.PeriodStart.IsZero() || b.PeriodEnd.IsZero() || !b.PeriodEnd.After(b.PeriodStart) || b.ExpiresAt.IsZero() || !b.ExpiresAt.After(b.PeriodStart) || b.ExpiresAt.After(b.PeriodEnd) {
			return Budget{}, ErrInvalid
		}
		selected = b
		count++
	}
	if err := rows.Err(); err != nil {
		return Budget{}, err
	}
	if count == 0 {
		return Budget{}, ErrNotFound
	}
	if count != 1 {
		return Budget{}, fmt.Errorf("%w: multiple active budgets match exact scope", ErrConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return Budget{}, err
	}
	return selected, nil
}
