package execute

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	transactioncommit "github.com/monstercameron/hcm-next/internal/transaction/commit"
	transactioncoordinator "github.com/monstercameron/hcm-next/internal/transaction/coordinator"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

type startRetryBindingBeginner struct{ began bool }

func (b *startRetryBindingBeginner) Begin(context.Context) (dbport.Tx, error) {
	b.began = true
	return nil, nil
}

func TestStartRetryFor_ReceivesExactRequestIdentityAndFactoryFailureHasNoEffects(t *testing.T) {
	b := &startRetryBindingBeginner{}
	tenant := uuid.New()
	wantErr := errors.New("budget selection refused")
	calls := 0
	d, err := New(Options{DB: b, Steps: startRetryBindingSteps{}, StartRetryFor: func(_ context.Context, req StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
		calls++
		if req.TenantID != tenant || req.StartIdempotencyKey != "start:exact" {
			t.Fatalf("factory identity tenant=%s key=%q", req.TenantID, req.StartIdempotencyKey)
		}
		return nil, wantErr
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Execute(context.Background(), ExecuteRequest{Start: runtime.StartRequest{TenantID: tenant, StartIdempotencyKey: "start:exact", Resolver: startRetryBindingResolver{}}})
	if !errors.Is(err, wantErr) || calls != 1 || b.began {
		t.Fatalf("err=%v calls=%d began=%v", err, calls, b.began)
	}
}

func TestStartRetryFor_ConcurrentRequestsDoNotShareIdentity(t *testing.T) {
	b := &startRetryBindingBeginner{}
	stop := errors.New("selected")
	type identity struct {
		tenant uuid.UUID
		key    string
	}
	var mu sync.Mutex
	seen := make(map[identity]int)
	d, err := New(Options{DB: b, Steps: startRetryBindingSteps{}, StartRetryFor: func(_ context.Context, req StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
		mu.Lock()
		seen[identity{tenant: req.TenantID, key: req.StartIdempotencyKey}]++
		mu.Unlock()
		return nil, stop
	}})
	if err != nil {
		t.Fatal(err)
	}
	requests := []identity{{uuid.New(), "start:a"}, {uuid.New(), "start:b"}}
	var wg sync.WaitGroup
	for _, request := range requests {
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, got := d.Execute(context.Background(), ExecuteRequest{Start: runtime.StartRequest{TenantID: request.tenant, StartIdempotencyKey: request.key, Resolver: startRetryBindingResolver{}}})
			if !errors.Is(got, stop) {
				t.Errorf("error=%v", got)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[requests[0]] != 1 || seen[requests[1]] != 1 || b.began {
		t.Fatalf("seen=%v began=%v", seen, b.began)
	}
}

type startRetryBindingSteps struct{}

func (startRetryBindingSteps) Run(context.Context, StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	panic("step runner must not be reached")
}

type startRetryBindingResolver struct{}

func (startRetryBindingResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	panic("resolver must not be reached")
}

func (startRetryBindingResolver) ResolveWorkflowInTx(context.Context, dbport.Tx, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	panic("resolver must not be reached")
}

func (b *startRetryBindingBeginner) BeginSerializable(context.Context) (dbport.Tx, error) {
	b.began = true
	return nil, errors.New("begin stopped")
}

func TestStartRetryFor_NilPolicyFailsClosedBeforeStart(t *testing.T) {
	b := &startRetryBindingBeginner{}
	d, err := New(Options{DB: b, Steps: startRetryBindingSteps{}, StartRetryFor: func(context.Context, StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Execute(context.Background(), ExecuteRequest{Start: runtime.StartRequest{TenantID: uuid.New(), StartIdempotencyKey: "start:nil-policy", Resolver: startRetryBindingResolver{}}})
	if err == nil {
		t.Fatal("expected a missing request policy to fail closed")
	}
	if b.began {
		t.Fatal("missing request policy opened a transaction")
	}
}

func TestStartRetryFor_RejectsStaticPolicy(t *testing.T) {
	_, err := New(Options{
		DB:         &startRetryBindingBeginner{},
		Steps:      startRetryBindingSteps{},
		StartRetry: &transactioncommit.RetryOptions{MaxAttempts: 2, Admit: func(context.Context) error { return nil }},
		StartRetryFor: func(context.Context, StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
			return &transactioncommit.RetryOptions{MaxAttempts: 2, Admit: func(context.Context) error { return nil }}, nil
		},
	})
	if err == nil {
		t.Fatal("expected static and request-scoped policies to be rejected together")
	}
}

func TestStartRetryFor_RejectsCoordinatorOnlyCallbacks(t *testing.T) {
	d, err := New(Options{DB: &startRetryBindingBeginner{}, Steps: startRetryBindingSteps{}, StartRetryFor: func(context.Context, StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
		return &transactioncommit.RetryOptions{MaxAttempts: 2, Admit: func(context.Context) error { return nil }, Prepare: func(context.Context, dbport.Tx) (transactioncoordinator.CommitRequest, error) {
			return transactioncoordinator.CommitRequest{}, nil
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(context.Background(), ExecuteRequest{Start: runtime.StartRequest{TenantID: uuid.New(), StartIdempotencyKey: "start:callbacks", Resolver: startRetryBindingResolver{}}}); err == nil {
		t.Fatal("expected coordinator-only callback to fail closed")
	}
}

func TestStartRetryFor_DoesNotMutateReturnedPolicy(t *testing.T) {
	policy := &transactioncommit.RetryOptions{Admit: func(context.Context) error { return nil }}
	d, err := New(Options{DB: &startRetryBindingBeginner{}, Steps: startRetryBindingSteps{}, StartRetryFor: func(context.Context, StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
		return policy, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = d.Execute(context.Background(), ExecuteRequest{Start: runtime.StartRequest{TenantID: uuid.New(), StartIdempotencyKey: "start:copy", Resolver: startRetryBindingResolver{}}})
	if policy.MaxAttempts != 0 || policy.BaseDelay != 0 || policy.MaxDelay != 0 || policy.Sleep != nil || policy.Jitter != nil {
		t.Fatalf("factory-owned policy mutated: %+v", policy)
	}
}
