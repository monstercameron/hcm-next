package admission

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func provisionSpec() ProvisionSpec {
	return ProvisionSpec{
		TenantID: "tenant-a", Service: "workflow", Dependency: "ledger",
		LogicalOperationID: "op/commit-1", OperationKind: "commit",
		Allowed: 3, Retryable: []FailureClass{FailureTransient, FailureTimeout}, Version: "v1",
	}
}

func attempt(id string, n int, failure FailureClass) AttemptInput {
	return attemptFor("op/commit-1", id, n, failure)
}

func attemptFor(operation, id string, n int, failure FailureClass) AttemptInput {
	return AttemptInput{
		AttemptID: id,
		Attempt: RetryAttempt{
			LogicalOperationID: operation, OperationKind: "commit",
			TenantID: "tenant-a", Dependency: "ledger", Failure: failure, Attempt: n,
		},
	}
}

// TestTodo_ADMISSION_002 is the ADMISSION-002 primary test: production
// budgets provision per logical operation, request-scoped attempts consume
// with replay-safe identities, exhaustion returns its repair route, and
// re-provisioning fabricates no allowance.
func TestTodo_ADMISSION_002_Provisioning(t *testing.T) {
	provisioner := NewProvisioner()
	budget, err := provisioner.Provision(provisionSpec())
	if err != nil {
		t.Fatal(err)
	}
	if budget.Allowed != 3 || budget.Consumed != 0 {
		t.Fatalf("provisioned = %+v, want 3 tokens unspent", budget)
	}

	first, err := provisioner.Consume(budget.ID, attempt("att-1", 1, FailureTransient))
	if err != nil {
		t.Fatal(err)
	}
	if first.Disposition != RetryAllowed || first.Remaining != 2 {
		t.Fatalf("first consume = %+v, want RETRY with 2 remaining", first)
	}
	// Replay-safe: the same attempt identity returns the stored receipt
	// without moving the counters.
	replay, err := provisioner.Consume(budget.ID, attempt("att-1", 1, FailureTransient))
	if err != nil {
		t.Fatal(err)
	}
	if replay != first {
		t.Fatalf("replay = %+v, want the identical stored receipt", replay)
	}
	snapshot, _ := provisioner.Snapshot(budget.ID)
	if snapshot.Consumed != 1 {
		t.Fatalf("consumed = %d after replay, want exactly 1", snapshot.Consumed)
	}

	if _, err := provisioner.Consume(budget.ID, attempt("att-2", 2, FailureTimeout)); err != nil {
		t.Fatal(err)
	}
	if _, err := provisioner.Consume(budget.ID, attempt("att-3", 3, FailureTransient)); err != nil {
		t.Fatal(err)
	}
	exhausted, err := provisioner.Consume(budget.ID, attempt("att-4", 4, FailureTransient))
	if err != nil {
		t.Fatal(err)
	}
	if exhausted.Disposition != RetryBudgetExhausted || exhausted.RepairRoute == "" {
		t.Fatalf("exhaustion = %+v, want RETRY_BUDGET_EXHAUSTED with a repair route", exhausted)
	}
	// The route is stable: further attempts keep the same disposition.
	again, err := provisioner.Consume(budget.ID, attempt("att-5", 5, FailureTimeout))
	if err != nil {
		t.Fatal(err)
	}
	if again.Disposition != RetryBudgetExhausted || again.RepairRoute != exhausted.RepairRoute {
		t.Fatalf("post-exhaustion = %+v, want the same exhausted route", again)
	}

	// Re-provisioning fabricates no allowance and resets nothing.
	same, err := provisioner.Provision(provisionSpec())
	if err != nil {
		t.Fatal(err)
	}
	if same.Consumed != 3 || same.Allowed != 3 {
		t.Fatalf("re-provisioned = %+v, want the untouched counters", same)
	}

	// A misclassified attempt refunds exactly once.
	refunded, err := provisioner.Refund(budget.ID, "att-3", "failure was permanent, not transient")
	if err != nil {
		t.Fatal(err)
	}
	if refunded.Refunded != 1 {
		t.Fatalf("refunded = %+v, want one token back", refunded)
	}
	if _, err := provisioner.Refund(budget.ID, "att-3", "again"); err == nil {
		t.Fatal("double refund accepted")
	}
}

func TestTodo_ADMISSION_002_ProvisioningRace(t *testing.T) {
	provisioner := NewProvisioner()
	spec := provisionSpec()
	spec.LogicalOperationID = "op/race"
	spec.Allowed = 16
	budget, err := provisioner.Provision(spec)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("race-%d", i)
			if _, err := provisioner.Consume(budget.ID, attemptFor("op/race", id, i+1, FailureTransient)); err != nil {
				errs <- err
				return
			}
			// Concurrent replays of the same identity consume once.
			if _, err := provisioner.Consume(budget.ID, attemptFor("op/race", id, i+1, FailureTransient)); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent consume = %v", err)
	}
	snapshot, _ := provisioner.Snapshot(budget.ID)
	if snapshot.Consumed != workers {
		t.Fatalf("consumed = %d, want exactly %d despite replays", snapshot.Consumed, workers)
	}
}

// TestTodo_ADMISSION_002_Integration wires the durable owner to the
// transaction coordinator's retry-only callback and to upstream
// scheduling: coordinator attempts consume provisioned tokens, and a slow
// downstream signal slows the scheduler before queues amplify.
func TestTodo_ADMISSION_002_ProvisioningIntegration(t *testing.T) {
	provisioner := NewProvisioner()
	budget, err := provisioner.Provision(provisionSpec())
	if err != nil {
		t.Fatal(err)
	}
	// The coordinator's retry-only callback: SQLState maps to the stable
	// failure class at this seam (mapping lives with the caller, never in
	// the retry scheduler), and every attempt spends one provisioned token.
	onRetry := func(ordinal int, sqlState string) error {
		failure := FailureTransient
		if sqlState == "53000" {
			failure = FailureThrottled
		}
		receipt, err := provisioner.Consume(budget.ID, attempt(fmt.Sprintf("coord-%d", ordinal), ordinal, failure))
		if err != nil {
			return err
		}
		if receipt.Disposition == RetryBudgetExhausted {
			return fmt.Errorf("coordinator: %s", receipt.RepairRoute)
		}
		if receipt.Disposition != RetryAllowed {
			return fmt.Errorf("coordinator: unexpected disposition %s", receipt.Disposition)
		}
		return nil
	}
	for n := 2; n <= 4; n++ {
		if err := onRetry(n, "40001"); err != nil {
			t.Fatalf("coordinator retry %d: %v", n, err)
		}
	}
	if err := onRetry(5, "40001"); err == nil {
		t.Fatal("coordinator retry past budget accepted")
	}
	snapshot, _ := provisioner.Snapshot(budget.ID)
	if snapshot.Consumed != 3 {
		t.Fatalf("coordinator consumed %d tokens, want 3", snapshot.Consumed)
	}

	// Upstream scheduling slows at the declared watermark instead of
	// queueing without bound.
	decision := DecideBackpressure(BackpressureSignal{
		Source: "connector", Dependency: "ledger", State: BackpressureSlow,
		QueueDepth: 40, QueueLimit: 100, Capacity: 50, RecommendedRate: 10,
	}, []string{"workflow", "connector"})
	if decision.Action != BackpressureSlowUpstream || decision.RecommendedRate != 10 {
		t.Fatalf("slow signal = %+v, want upstream SLOW at rate 10", decision)
	}
	over := DecideBackpressure(BackpressureSignal{
		Source: "connector", Dependency: "ledger", State: BackpressureSlow,
		QueueDepth: 101, QueueLimit: 100, Capacity: 50, RecommendedRate: 10, RetryAfter: 5,
	}, []string{"workflow"})
	if over.Action != BackpressureDefer {
		t.Fatalf("watermark breach = %+v, want DEFER", over)
	}
}

func TestTodo_ADMISSION_002_ProvisioningFault(t *testing.T) {
	provisioner := NewProvisioner()
	budget, err := provisioner.Provision(provisionSpec())
	if err != nil {
		t.Fatal(err)
	}

	// A non-retryable failure never spends a token.
	narrow, err := provisioner.Provision(ProvisionSpec{
		TenantID: "tenant-a", Service: "workflow", Dependency: "ledger",
		LogicalOperationID: "op/narrow", OperationKind: "commit",
		Allowed: 2, Retryable: []FailureClass{FailureTransient}, Version: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	refused, err := provisioner.Consume(narrow.ID, AttemptInput{
		AttemptID: "att-timeout",
		Attempt: RetryAttempt{
			LogicalOperationID: "op/narrow", OperationKind: "commit",
			TenantID: "tenant-a", Dependency: "ledger", Failure: FailureTimeout, Attempt: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if refused.Disposition != RetryNotAllowed {
		t.Fatalf("non-retryable failure = %+v, want NOT_ALLOWED", refused)
	}
	snapshot, _ := provisioner.Snapshot(narrow.ID)
	if snapshot.Consumed != 0 {
		t.Fatalf("refused attempt spent a token: %+v", snapshot)
	}
	// Cross-operation and cross-tenant attempts are invalid input.
	for name, input := range map[string]AttemptInput{
		"foreign operation": {AttemptID: "x1", Attempt: RetryAttempt{LogicalOperationID: "op/other", OperationKind: "commit", TenantID: "tenant-a", Dependency: "ledger", Failure: FailureTransient, Attempt: 1}},
		"foreign tenant":    {AttemptID: "x2", Attempt: RetryAttempt{LogicalOperationID: "op/commit-1", OperationKind: "commit", TenantID: "tenant-b", Dependency: "ledger", Failure: FailureTransient, Attempt: 1}},
		"blank identity":    {Attempt: RetryAttempt{LogicalOperationID: "op/commit-1", OperationKind: "commit", TenantID: "tenant-a", Dependency: "ledger", Failure: FailureTransient, Attempt: 1}},
		"unknown budget":    {AttemptID: "x3", Attempt: RetryAttempt{LogicalOperationID: "op/commit-1", OperationKind: "commit", TenantID: "tenant-a", Dependency: "ledger", Failure: FailureTransient, Attempt: 1}},
	} {
		input := input
		_, err := provisioner.Consume(budget.ID, input)
		if name == "unknown budget" {
			_, err = provisioner.Consume("budget/tenant-a/op/ghost", input)
		}
		if !errors.Is(err, ErrInvalidRetryInput) {
			t.Fatalf("%s = %v, want ErrInvalidRetryInput", name, err)
		}
	}
	// Refunds need a consumed attempt and a reason.
	if _, err := provisioner.Refund(budget.ID, "att/ghost", "reason"); !errors.Is(err, ErrInvalidRetryInput) {
		t.Fatalf("ghost refund = %v, want ErrInvalidRetryInput", err)
	}
	if _, err := provisioner.Refund(budget.ID, "att/ghost", ""); !errors.Is(err, ErrInvalidRetryInput) {
		t.Fatalf("reasonless refund = %v, want ErrInvalidRetryInput", err)
	}
	// Provisioning without identity, allowance meaning or retryable set.
	for name, spec := range map[string]ProvisionSpec{
		"blank tenant": {Service: "s", Dependency: "d", LogicalOperationID: "op/1", OperationKind: "k", Allowed: 1, Retryable: []FailureClass{FailureTransient}, Version: "v1"},
		"no retryable": {TenantID: "t", Service: "s", Dependency: "d", LogicalOperationID: "op/1", OperationKind: "k", Allowed: 1, Version: "v1"},
	} {
		if _, err := provisioner.Provision(spec); !errors.Is(err, ErrInvalidRetryInput) {
			t.Fatalf("%s provision = %v, want ErrInvalidRetryInput", name, err)
		}
	}
}

func BenchmarkTodo_ADMISSION_002_Provisioning(b *testing.B) {
	provisioner := NewProvisioner()
	budget, err := provisioner.Provision(provisionSpec())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-%d", i)
		if _, err := provisioner.Consume(budget.ID, attempt(id, 1, FailureTransient)); err != nil {
			b.Fatal(err)
		}
	}
}
