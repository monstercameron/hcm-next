package delivery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type outageProvider struct{}

func (outageProvider) Send(context.Context, Delivery) (ProviderResult, error) {
	return ProviderResult{}, errors.New("provider outage")
}

func reconcilePolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 5, BackoffTicks: 30, FallbackAfter: 3}
}

func TestTodo_MSG_009(t *testing.T) {
	// First outage retries within bound with a scheduled tick.
	first, err := Reconcile("intent-1", "worker:w1", "INTERNAL", "leave-notice", FailureOutage, 0, 100, reconcilePolicy())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if first.Outcome != ReconcileRetry || first.NextTick != 130 {
		t.Fatalf("first=%+v", first)
	}
	if first.Obligation.Satisfied || !first.Obligation.Visible || first.Obligation.Digest == "" {
		t.Fatalf("obligation=%+v", first.Obligation)
	}
	// Exhausted attempts fall back to the secure inbox with identical
	// recipient and classification.
	third, err := Reconcile("intent-1", "worker:w1", "INTERNAL", "leave-notice", FailureOutage, 2, 100, reconcilePolicy())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if third.Outcome != ReconcileFallbackInbox {
		t.Fatalf("third=%+v", third)
	}
	if third.Fallback.RecipientRef != "worker:w1" || third.Fallback.Classification != "INTERNAL" {
		t.Fatalf("fallback broadened: %+v", third.Fallback)
	}
	// Invalid endpoints never retry: straight to a human task.
	dead, err := Reconcile("intent-2", "worker:w2", "INTERNAL", "leave-notice", FailureInvalidEndpoint, 0, 100, reconcilePolicy())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if dead.Outcome != ReconcileFallbackTask || dead.Fallback.TaskAssignee == "" {
		t.Fatalf("dead=%+v", dead)
	}
	// Restricted material never assumes an alternate channel.
	restricted, err := Reconcile("intent-3", "worker:w3", "RESTRICTED", "leave-notice", FailureBounce, 4, 100, reconcilePolicy())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if restricted.Outcome != ReconcileFallbackTask {
		t.Fatalf("restricted=%+v", restricted)
	}
	// RED: hollow inputs and unbounded policies refuse.
	if _, err := Reconcile("", "worker:w1", "INTERNAL", "p", FailureOutage, 0, 100, reconcilePolicy()); err == nil {
		t.Fatal("hollow intent reconciled")
	}
	if _, err := Reconcile("i", "w", "C", "p", "vibes", 0, 100, reconcilePolicy()); err == nil {
		t.Fatal("unknown failure reconciled")
	}
	if _, err := Reconcile("i", "w", "C", "p", FailureOutage, 0, 100, RetryPolicy{}); err == nil {
		t.Fatal("unbounded policy reconciled")
	}
}

func TestTodo_MSG_009_Race(t *testing.T) {
	reconciler := NewReconciler()
	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			intentID := "intent-race"
			if i%2 == 0 {
				intentID = "intent-race-2"
			}
			reconciliation, err := Reconcile(intentID, "worker:w1", "INTERNAL", "leave-notice", FailureOutage, 2, 100, reconcilePolicy())
			if err != nil {
				errs[i] = err
				return
			}
			reconciler.Record(reconciliation)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
	}
	visible := reconciler.Visible()
	if len(visible) != 2 {
		t.Fatalf("visible = %d obligations, want both unsatisfied intents", len(visible))
	}
}

func TestTodo_MSG_009_Integration(t *testing.T) {
	// A real dispatch through a failing provider produces the failed
	// attempt that reconciliation consumes: the obligation stays
	// unsatisfied and visible, never silent.
	dispatcher, err := NewDispatcher(outageProvider{}, Policy{})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	result, err := dispatcher.Dispatch(context.Background(), Intent{
		TenantID: "acme", IntentID: "intent-9", RecipientRef: "worker:w9",
		Purpose: "leave-notice", Subject: "Leave update", Body: "Update.",
		Classification: "INTERNAL", IdempotencyKey: "msg-9",
		CanonicalRequest: []byte("leave-update-9"), Committed: true, CreatedAt: time.Unix(0, 0).UTC(),
	})
	if err == nil || result.Attempt.State != Failed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	reconciliation, err := Reconcile("intent-9", "worker:w9", "INTERNAL", "leave-notice", FailureOutage, 0, 100, reconcilePolicy())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if reconciliation.Outcome != ReconcileRetry || reconciliation.Obligation.Satisfied {
		t.Fatalf("reconciliation=%+v", reconciliation)
	}
	reconciler := NewReconciler()
	reconciler.Record(reconciliation)
	if len(reconciler.Visible()) != 1 {
		t.Fatal("failed delivery left no visible obligation")
	}
}

func TestTodo_MSG_009_Fault(t *testing.T) {
	// Bounces behave like outages within bound.
	bounce, err := Reconcile("intent-b", "worker:w1", "INTERNAL", "p", FailureBounce, 1, 100, reconcilePolicy())
	if err != nil || bounce.Outcome != ReconcileRetry {
		t.Fatalf("bounce=%+v err=%v", bounce, err)
	}
	// Attempts past the maximum still terminate at fallback: retries
	// never run forever.
	past, err := Reconcile("intent-b", "worker:w1", "INTERNAL", "p", FailureOutage, 99, 100, reconcilePolicy())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if past.Outcome == ReconcileRetry {
		t.Fatal("unbounded retry scheduled")
	}
}
