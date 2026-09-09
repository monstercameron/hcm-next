package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/admissionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	workflowexecute "github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// composeExecutionRetryFor returns a request-scoped selector for the durable
// START retry budget. Selection is repeated for every Execute call and the
// returned admission rechecks the persisted budget on each retry consumption.
func composeExecutionRetryFor(pool *pgxadapter.Pool, cfg ServeConfig, now func() time.Time) func(context.Context, workflowexecute.StartRetryIdentity) (*commit.RetryOptions, error) {
	store := admissionstore.New(pool)
	return func(ctx context.Context, identity workflowexecute.StartRetryIdentity) (*commit.RetryOptions, error) {
		at := now().UTC()
		budget, err := store.SelectActiveBudget(ctx, identity.TenantID.String(), "workflow", "postgres", identity.StartIdempotencyKey, "start", cfg.ExecutionRetryVersion, at)
		if err != nil {
			return nil, fmt.Errorf("select execution retry budget: %w", err)
		}
		admission, err := platformexecution.NewRetryAdmission(runtime.StartRequest{TenantID: identity.TenantID, StartIdempotencyKey: identity.StartIdempotencyKey}, platformexecution.RetryBudgetSpec{Store: store, BudgetID: budget.BudgetID, Service: budget.Service, Dependency: budget.Dependency, OperationKind: budget.OperationKind, BudgetVersion: budget.Version, Now: now, MaxResolutionAttempts: cfg.ExecutionRetryResolutionAttempts})
		if err != nil {
			return nil, err
		}
		admit := func(ctx context.Context) error {
			current, err := store.SelectActiveBudget(ctx, identity.TenantID.String(), budget.Service, budget.Dependency, budget.LogicalOperationID, budget.OperationKind, budget.Version, now().UTC())
			if err != nil {
				return fmt.Errorf("revalidate execution retry budget: %w", err)
			}
			if !sameRetryBudget(current, budget) {
				return fmt.Errorf("revalidate execution retry budget: %w", admissionstore.ErrConflict)
			}
			return nil
		}
		return &commit.RetryOptions{MaxAttempts: cfg.ExecutionRetryMaxAttempts, Admit: admit, OnRetry: admission.OnRetry}, nil
	}
}

func sameRetryBudget(a, b admissionstore.Budget) bool {
	return a.BudgetID == b.BudgetID && a.TenantID == b.TenantID && a.Service == b.Service && a.Dependency == b.Dependency && a.LogicalOperationID == b.LogicalOperationID && a.OperationKind == b.OperationKind && a.PeriodStart.Equal(b.PeriodStart) && a.PeriodEnd.Equal(b.PeriodEnd) && a.ExpiresAt.Equal(b.ExpiresAt) && a.Allowed == b.Allowed && a.Refunded == b.Refunded && slices.Equal(a.Retryable, b.Retryable) && a.Version == b.Version && a.Owner == b.Owner
}
