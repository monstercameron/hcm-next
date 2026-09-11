package runtime_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func retryPolicy() runtime.RetryPolicy {
	return runtime.RetryPolicy{
		MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second,
		Deadline:  10 * time.Second,
		Retryable: []runtime.FailureKind{runtime.FailureTransient, runtime.FailureTimeout},
		Jitter:    func(attempt int, delay time.Duration) time.Duration { return delay },
	}
}

func retryBudget(t *testing.T, provisioner *admission.Provisioner, operation string, allowed int) runtime.BudgetRef {
	t.Helper()
	budget, err := provisioner.Provision(admission.ProvisionSpec{
		TenantID: "tenant-a", Service: "workflow", Dependency: "ledger",
		LogicalOperationID: operation, OperationKind: "node",
		Allowed: allowed, Retryable: []admission.FailureClass{admission.FailureTransient, admission.FailureTimeout}, Version: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime.BudgetRef{
		Provisioner: provisioner, BudgetID: budget.ID, TenantID: "tenant-a",
		LogicalOperationID: operation, OperationKind: "node", Dependency: "ledger",
	}
}

func retryAttempt(n int, failure runtime.FailureKind, id string) runtime.RetryAttempt {
	return runtime.RetryAttempt{N: n, Failure: failure, AttemptID: id, Layer: "execute"}
}

// TestTodo_WF_RUN_006 is the WF-RUN-006 primary test: the classifier,
// attempts cap, backoff, deadline and propagated budget route every
// failure to its exact next attempt or terminal reason.
func TestTodo_WF_RUN_006(t *testing.T) {
	provisioner := admission.NewProvisioner()
	budget := retryBudget(t, provisioner, "op/retry-1", 3)

	// A transient failure schedules attempt 2 at exactly the base delay
	// with the budget spending one token.
	route, err := runtime.Decide(retryPolicy(), budget, retryAttempt(1, runtime.FailureTransient, "wf-1"))
	if err != nil {
		t.Fatal(err)
	}
	if route.Decision != runtime.RouteRetry || route.NextAttempt != 2 || route.NextDelay != 100*time.Millisecond || route.BudgetRemaining != 2 {
		t.Fatalf("route = %+v, want RETRY attempt 2 after 100ms with 2 tokens left", route)
	}
	// Backoff doubles per attempt: attempt 3 waits 200ms.
	route, err = runtime.Decide(retryPolicy(), budget, retryAttempt(2, runtime.FailureTimeout, "wf-2"))
	if err != nil {
		t.Fatal(err)
	}
	if route.Decision != runtime.RouteRetry || route.NextDelay != 200*time.Millisecond {
		t.Fatalf("route = %+v, want RETRY after 200ms", route)
	}
	// Terminal routes spend nothing and name their reason.
	terminals := []struct {
		name    string
		attempt runtime.RetryAttempt
		reason  string
	}{
		{"nonretryable", retryAttempt(1, runtime.FailurePermanent, "wf-p"), runtime.ReasonNonretryable},
		{"do-not-retry", runtime.RetryAttempt{N: 1, Failure: runtime.FailureTransient, DoNotRetry: true, AttemptID: "wf-dnr"}, runtime.ReasonDoNotRetry},
		{"attempts exhausted", retryAttempt(3, runtime.FailureTransient, "wf-cap"), runtime.ReasonAttemptsExhausted},
		{"deadline exceeded", runtime.RetryAttempt{N: 1, Failure: runtime.FailureTransient, Elapsed: 9950 * time.Millisecond, AttemptID: "wf-dl"}, runtime.ReasonDeadlineExceeded},
	}
	for _, tc := range terminals {
		route, err := runtime.Decide(retryPolicy(), budget, tc.attempt)
		if err != nil {
			t.Fatal(err)
		}
		if route.Decision != runtime.RouteTerminal || route.Reason != tc.reason {
			t.Fatalf("%s route = %+v, want TERMINAL %s", tc.name, route, tc.reason)
		}
	}
	snapshot, _ := provisioner.Snapshot(budget.BudgetID)
	if snapshot.Consumed != 2 {
		t.Fatalf("consumed = %d, want exactly the 2 scheduled retries", snapshot.Consumed)
	}

	// The spent budget exhausts with its repair route.
	if _, err := runtime.Decide(retryPolicy(), budget, retryAttempt(2, runtime.FailureTransient, "wf-3")); err != nil {
		t.Fatal(err)
	}
	route, err = runtime.Decide(retryPolicy(), budget, retryAttempt(1, runtime.FailureTransient, "wf-4"))
	if err != nil {
		t.Fatal(err)
	}
	if route.Decision != runtime.RouteTerminal || route.Reason != runtime.ReasonBudgetExhausted || route.RepairRoute == "" {
		t.Fatalf("exhausted route = %+v, want TERMINAL BUDGET_EXHAUSTED with a route", route)
	}

	// Nested layers share one budget: inner and outer attempts draw from
	// the same tokens instead of multiplying.
	nested := admission.NewProvisioner()
	shared := retryBudget(t, nested, "op/nested", 2)
	inner, err := runtime.Decide(retryPolicy(), shared, runtime.RetryAttempt{N: 1, Failure: runtime.FailureTransient, AttemptID: "inner-1", Layer: "step/inner"})
	if err != nil || inner.Decision != runtime.RouteRetry {
		t.Fatalf("inner = %+v, %v", inner, err)
	}
	outer, err := runtime.Decide(retryPolicy(), shared, runtime.RetryAttempt{N: 1, Failure: runtime.FailureTransient, AttemptID: "outer-1", Layer: "step/outer"})
	if err != nil || outer.Decision != runtime.RouteRetry {
		t.Fatalf("outer = %+v, %v", outer, err)
	}
	third, err := runtime.Decide(retryPolicy(), shared, runtime.RetryAttempt{N: 1, Failure: runtime.FailureTransient, AttemptID: "third-1", Layer: "step/inner"})
	if err != nil {
		t.Fatal(err)
	}
	if third.Decision != runtime.RouteTerminal || third.Reason != runtime.ReasonBudgetExhausted {
		t.Fatalf("third nested attempt = %+v, want shared-budget exhaustion", third)
	}
}

func TestTodo_WF_RUN_006_Race(t *testing.T) {
	provisioner := admission.NewProvisioner()
	budget := retryBudget(t, provisioner, "op/race", 16)
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			route, err := runtime.Decide(retryPolicy(), budget, retryAttempt(1, runtime.FailureTransient, fmt.Sprintf("race-%d", i)))
			if err != nil {
				errs <- err
				return
			}
			if route.Decision != runtime.RouteRetry {
				errs <- fmt.Errorf("worker %d route = %+v", i, route)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent decide = %v", err)
	}
	snapshot, _ := provisioner.Snapshot(budget.BudgetID)
	if snapshot.Consumed != workers {
		t.Fatalf("consumed = %d, want exactly %d", snapshot.Consumed, workers)
	}
}

func TestTodo_WF_RUN_006_Fault(t *testing.T) {
	provisioner := admission.NewProvisioner()
	budget := retryBudget(t, provisioner, "op/fault", 2)

	// Invalid policies and missing identities are caller errors, never
	// silent terminals.
	broken := retryPolicy()
	broken.MaxAttempts = 0
	if _, err := runtime.Decide(broken, budget, retryAttempt(1, runtime.FailureTransient, "f-1")); err == nil {
		t.Fatal("zero-attempt policy accepted")
	}
	if _, err := runtime.Decide(retryPolicy(), budget, retryAttempt(1, runtime.FailureTransient, "")); err == nil {
		t.Fatal("identity-less attempt accepted")
	}
	if _, err := runtime.Decide(retryPolicy(), runtime.BudgetRef{}, retryAttempt(1, runtime.FailureTransient, "f-2")); err == nil {
		t.Fatal("budget-less routing accepted")
	}
	ghost := budget
	ghost.BudgetID = "budget/tenant-a/op/ghost"
	if _, err := runtime.Decide(retryPolicy(), ghost, retryAttempt(1, runtime.FailureTransient, "f-3")); err == nil {
		t.Fatal("unknown budget accepted")
	}
	// A mismatched operation scope is invalid input from the provisioner.
	scoped := budget
	scoped.LogicalOperationID = "op/other"
	if _, err := runtime.Decide(retryPolicy(), scoped, retryAttempt(1, runtime.FailureTransient, "f-4")); err == nil {
		t.Fatal("scope-mismatched routing accepted")
	}
	// No budget moved on any fault.
	snapshot, _ := provisioner.Snapshot(budget.BudgetID)
	if snapshot.Consumed != 0 {
		t.Fatalf("faults consumed %d tokens", snapshot.Consumed)
	}
}

func TestTodo_WF_RUN_006_Mutation(t *testing.T) {
	provisioner := admission.NewProvisioner()
	budget := retryBudget(t, provisioner, "op/mutation", 4)

	// Mutant 1: jitter cannot escape the cap.
	spiky := retryPolicy()
	spiky.Jitter = func(attempt int, delay time.Duration) time.Duration { return 10 * time.Second }
	route, err := runtime.Decide(spiky, budget, retryAttempt(1, runtime.FailureTransient, "m-1"))
	if err != nil {
		t.Fatal(err)
	}
	if route.NextDelay != time.Second {
		t.Fatalf("jittered delay = %v, want the 1s cap", route.NextDelay)
	}
	// Mutant 2: replaying an attempt identity returns the stored route
	// without spending again.
	first, err := runtime.Decide(retryPolicy(), budget, retryAttempt(1, runtime.FailureTransient, "m-2"))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runtime.Decide(retryPolicy(), budget, retryAttempt(1, runtime.FailureTransient, "m-2"))
	if err != nil {
		t.Fatal(err)
	}
	if replay != first {
		t.Fatalf("replay = %+v, want the identical stored route", replay)
	}
	snapshot, _ := provisioner.Snapshot(budget.BudgetID)
	if snapshot.Consumed != 2 {
		t.Fatalf("consumed = %d, want 2 (spiky + first, replay free)", snapshot.Consumed)
	}
	// Mutant 3: an unlisted failure kind is nonretryable even when the
	// budget is full.
	route, err = runtime.Decide(retryPolicy(), budget, retryAttempt(1, runtime.FailureKind("BYZANTINE"), "m-3"))
	if err != nil {
		t.Fatal(err)
	}
	if route.Decision != runtime.RouteTerminal || route.Reason != runtime.ReasonNonretryable {
		t.Fatalf("unknown kind = %+v, want TERMINAL NONRETRYABLE", route)
	}
	// Mutant 4: the deadline boundary is exact: elapsed plus delay equal
	// to the deadline still routes (100ms elapsed + 100ms backoff).
	edge := retryPolicy()
	edge.Deadline = 200 * time.Millisecond
	route, err = runtime.Decide(edge, budget, runtime.RetryAttempt{N: 0, Failure: runtime.FailureTransient, Elapsed: 100 * time.Millisecond, AttemptID: "m-4"})
	if err != nil {
		t.Fatal(err)
	}
	if route.Decision != runtime.RouteRetry || route.NextDelay != 100*time.Millisecond {
		t.Fatalf("boundary route = %+v, want RETRY after 100ms", route)
	}
}
