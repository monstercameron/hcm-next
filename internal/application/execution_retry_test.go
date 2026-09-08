package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/hcm-next/internal/data/admissionstore"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/operations/admission"
	workflowexecute "github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

func TestTodo_ADMISSION_002_ComposedRetrySelectorFailsClosedWithoutPool(t *testing.T) {
	cfg := ServeConfig{ExecutionRetryVersion: "v1", ExecutionRetryResolutionAttempts: 2, ExecutionRetryMaxAttempts: 2}
	selector := composeExecutionRetryFor(nil, cfg, func() time.Time { return time.Now().UTC() })
	_, err := selector(context.Background(), workflowexecute.StartRetryIdentity{TenantID: uuid.New(), StartIdempotencyKey: "start-1"})
	if err == nil {
		t.Fatal("retry selector unexpectedly composed without a durable admission store")
	}
}

func TestTodo_ADMISSION_002_DisabledRetryLeavesLegacyConfigurationValid(t *testing.T) {
	cfg := stubServeConfig()
	cfg.ExecutionRetry = false
	cfg.ExecutionRetryMaxAttempts = 1
	cfg.ExecutionRetryResolutionAttempts = 1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled retry configuration rejected: %v", err)
	}
}

type retryCompositionDB struct {
	begins int
	errs   []error
}

func (d *retryCompositionDB) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("unexpected legacy begin")
}
func (d *retryCompositionDB) BeginSerializable(context.Context) (dbport.Tx, error) {
	d.begins++
	if len(d.errs) == 0 {
		return nil, errors.New("start stopped after durable admission")
	}
	err := d.errs[0]
	d.errs = d.errs[1:]
	return nil, err
}

type retryCompositionSteps struct{}

func (retryCompositionSteps) Run(context.Context, workflowexecute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	panic("step must not run")
}

type retryCompositionResolver struct{}

func (retryCompositionResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	panic("non-transactional resolution used")
}
func (retryCompositionResolver) ResolveWorkflowInTx(context.Context, dbport.Tx, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	panic("begin failure must precede resolution")
}

func TestTodo_ADMISSION_002_ComposedRetryFactoryRevalidatesPersistedBudgetBeforeStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	budget := admissionstore.Budget{BudgetID: uuid.New(), TenantID: uuid.New(), Service: "workflow", Dependency: "postgres", LogicalOperationID: "start:composed", OperationKind: "start", PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour), ExpiresAt: now.Add(time.Hour), Allowed: 1, Retryable: []admission.FailureClass{admission.FailureTransient}, Version: "v1", Owner: "application-test"}
	if err := admissionstore.New(pool).CreateBudget(ctx, budget); err != nil {
		t.Fatal(err)
	}
	cfg := ServeConfig{ExecutionRetryVersion: budget.Version, ExecutionRetryMaxAttempts: 2, ExecutionRetryResolutionAttempts: 2}
	startDB := &retryCompositionDB{errs: []error{&pgconn.PgError{Code: "40001", Message: "known abort"}, errors.New("second start stopped")}}
	driver, err := workflowexecute.New(workflowexecute.Options{DB: startDB, Steps: retryCompositionSteps{}, StartRetryFor: composeExecutionRetryFor(pool, cfg, func() time.Time { return now })})
	if err != nil {
		t.Fatal(err)
	}
	request := workflowexecute.ExecuteRequest{Start: runtime.StartRequest{TenantID: budget.TenantID, StartIdempotencyKey: budget.LogicalOperationID, Resolver: retryCompositionResolver{}}}
	if _, err := driver.Execute(ctx, request); err == nil || startDB.begins != 2 {
		t.Fatalf("err=%v serializable begins=%d", err, startDB.begins)
	}
	var consumed, receipts int
	if err := db.Conn.QueryRow(ctx, `SELECT consumed FROM admission_retry_budget WHERE budget_id=$1`, budget.BudgetID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM admission_retry_receipt WHERE budget_id=$1`, budget.BudgetID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if consumed != 1 || receipts != 1 {
		t.Fatalf("consumed=%d receipts=%d, want 1/1", consumed, receipts)
	}

	startDB.begins = 0
	startDB.errs = []error{&pgconn.PgError{Code: "40001", Message: "known abort"}, errors.New("must not be reached")}
	if _, err := driver.Execute(ctx, request); !errors.Is(err, admissionstore.ErrExhausted) || startDB.begins != 1 {
		t.Fatalf("exhausted retry err=%v begins=%d", err, startDB.begins)
	}

	clockCalls := 0
	expiring := composeExecutionRetryFor(pool, cfg, func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return now
		}
		return now.Add(2 * time.Hour)
	})
	startDB.begins = 0
	driver, err = workflowexecute.New(workflowexecute.Options{DB: startDB, Steps: retryCompositionSteps{}, StartRetryFor: expiring})
	if err != nil {
		t.Fatal(err)
	}
	_, err = driver.Execute(ctx, request)
	if !errors.Is(err, admissionstore.ErrNotFound) || startDB.begins != 0 {
		t.Fatalf("expired revalidation err=%v begins=%d", err, startDB.begins)
	}

	startDB.begins = 0
	driver, err = workflowexecute.New(workflowexecute.Options{DB: startDB, Steps: retryCompositionSteps{}, StartRetryFor: composeExecutionRetryFor(pool, cfg, func() time.Time { return now })})
	if err != nil {
		t.Fatal(err)
	}
	request.Start.StartIdempotencyKey = "start:wrong-scope"
	_, err = driver.Execute(ctx, request)
	if !errors.Is(err, admissionstore.ErrNotFound) || startDB.begins != 0 {
		t.Fatalf("wrong-scope selection err=%v begins=%d", err, startDB.begins)
	}
}
