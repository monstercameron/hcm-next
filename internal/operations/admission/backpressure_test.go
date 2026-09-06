package admission

import (
	"errors"
	"testing"
)

func TestTodo_ADMISSION_002(t *testing.T) {
	signal := BackpressureSignal{Source: "workflow", Dependency: "connector", State: BackpressureThrottled, QueueDepth: 8, QueueLimit: 10, RetryAfter: 4}
	decision := DecideBackpressure(signal, []string{"message", "workflow", "workflow"})
	if decision.Action != BackpressureQueue || decision.RetryAfter != 4 || len(decision.Targets) != 2 {
		t.Fatalf("backpressure decision = %+v", decision)
	}

	budget := RetryBudget{ID: "budget-1", TenantID: "tenant-a", Dependency: "connector", Allowed: 1, Retryable: []FailureClass{FailureUnavailable}, Version: "v1"}
	attempt := RetryAttempt{LogicalOperationID: "op-1", TenantID: "tenant-a", Dependency: "connector", Failure: FailureUnavailable, Attempt: 1}
	receipt, err := ConsumeRetry(budget, attempt)
	if err != nil || receipt.Disposition != RetryAllowed || receipt.Remaining != 0 || receipt.Consumed != 1 {
		t.Fatalf("retry receipt = %+v, err=%v", receipt, err)
	}
	budget.Consumed = 1
	receipt, err = ConsumeRetry(budget, attempt)
	if err != nil || receipt.Disposition != RetryBudgetExhausted || receipt.RepairRoute == "" {
		t.Fatalf("exhausted receipt = %+v, err=%v", receipt, err)
	}
}

func TestTodo_ADMISSION_002_Race(t *testing.T) {
	signal := BackpressureSignal{Source: "workflow", Dependency: "messages", State: BackpressureSlow, RecommendedRate: 12}
	want := DecideBackpressure(signal, []string{"b", "a"})
	for i := 0; i < 100; i++ {
		got := DecideBackpressure(signal, []string{"a", "b"})
		if got.Action != want.Action || got.Reason != want.Reason || got.RecommendedRate != want.RecommendedRate || len(got.Targets) != len(want.Targets) {
			t.Fatalf("non-deterministic propagation: got=%+v want=%+v", got, want)
		}
	}
}

func TestTodo_ADMISSION_002_Integration(t *testing.T) {
	decision := DecideBackpressure(BackpressureSignal{Source: "workflow", Dependency: "connector", QueueDepth: 11, QueueLimit: 10}, []string{"connector", "workflow"})
	if decision.Action != BackpressureDefer || decision.Reason != "QUEUE_WATERMARK_EXCEEDED" {
		t.Fatalf("watermark decision = %+v", decision)
	}
}

func TestTodo_ADMISSION_002_Fault(t *testing.T) {
	decision := DecideBackpressure(BackpressureSignal{Source: "", Dependency: "connector", State: BackpressureHealthy}, nil)
	if decision.Action != BackpressureStop || decision.Reason != "INVALID_SIGNAL" {
		t.Fatalf("invalid signal decision = %+v", decision)
	}
}

func BenchmarkTodo_ADMISSION_002(b *testing.B) {
	signal := BackpressureSignal{Source: "workflow", Dependency: "connector", State: BackpressureSlow, RecommendedRate: 12}
	for i := 0; i < b.N; i++ {
		_ = DecideBackpressure(signal, []string{"workflow", "connector"})
	}
}

func TestBackpressureDecisionStatesAndBounds(t *testing.T) {
	base := BackpressureSignal{Source: "workflow", Dependency: "connector", QueueLimit: 10, Capacity: 20}
	for _, tc := range []struct {
		name   string
		state  BackpressureState
		depth  int
		retry  int
		want   BackpressureAction
		reason string
	}{
		{"empty state healthy", "", 0, 0, BackpressureContinue, "DOWNSTREAM_HEALTHY"},
		{"healthy", BackpressureHealthy, 0, 0, BackpressureContinue, "DOWNSTREAM_HEALTHY"},
		{"slow", BackpressureSlow, 0, 0, BackpressureSlowUpstream, "DOWNSTREAM_SLOW"},
		{"throttled fallback", BackpressureThrottled, 0, 0, BackpressureQueue, "DOWNSTREAM_THROTTLED"},
		{"unavailable fallback", BackpressureUnavailable, 0, 0, BackpressureDefer, "DOWNSTREAM_UNAVAILABLE"},
		{"unknown state", BackpressureState("BROKEN"), 0, 0, BackpressureStop, "UNKNOWN_DOWNSTREAM_STATE"},
		{"watermark takes precedence", BackpressureHealthy, 11, 4, BackpressureDefer, "QUEUE_WATERMARK_EXCEEDED"},
		{"negative signal", BackpressureHealthy, -1, 4, BackpressureStop, "INVALID_SIGNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			s.State, s.QueueDepth, s.RetryAfter = tc.state, tc.depth, tc.retry
			got := DecideBackpressure(s, []string{" workflow ", "", "workflow", "connector"})
			if got.Action != tc.want || got.Reason != tc.reason {
				t.Fatalf("decision=%+v", got)
			}
			if len(got.Targets) != 2 || got.Targets[0] != "connector" || got.Targets[1] != "workflow" {
				t.Fatalf("targets=%v", got.Targets)
			}
		})
	}
}

func TestConsumeRetryRejectsInvalidAndHandlesNonRetryableFailures(t *testing.T) {
	valid := RetryBudget{ID: "budget", TenantID: "tenant", Dependency: "connector", Allowed: 2, Retryable: []FailureClass{FailureUnavailable}, Version: "v1"}
	attempt := RetryAttempt{LogicalOperationID: "op", TenantID: "tenant", Dependency: "connector", Failure: FailureUnavailable, Attempt: 1}
	for _, tc := range []struct {
		name   string
		mutate func(*RetryBudget, *RetryAttempt)
	}{
		{"missing budget id", func(b *RetryBudget, _ *RetryAttempt) { b.ID = "" }},
		{"tenant mismatch", func(_ *RetryBudget, a *RetryAttempt) { a.TenantID = "other" }},
		{"dependency mismatch", func(_ *RetryBudget, a *RetryAttempt) { a.Dependency = "other" }},
		{"zero attempt", func(_ *RetryBudget, a *RetryAttempt) { a.Attempt = 0 }},
		{"negative consumed", func(b *RetryBudget, _ *RetryAttempt) { b.Consumed = -1 }},
		{"counter exceeds budget", func(b *RetryBudget, _ *RetryAttempt) { b.Consumed = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, a := valid, attempt
			tc.mutate(&b, &a)
			if _, err := ConsumeRetry(b, a); !errors.Is(err, ErrInvalidRetryInput) {
				t.Fatalf("ConsumeRetry=%v", err)
			}
		})
	}
	notRetryable := attempt
	notRetryable.Failure = FailureTimeout
	receipt, err := ConsumeRetry(valid, notRetryable)
	if err != nil || receipt.Disposition != RetryNotAllowed || receipt.Remaining != 2 || receipt.Consumed != 0 {
		t.Fatalf("non-retryable receipt=%+v err=%v", receipt, err)
	}
}
