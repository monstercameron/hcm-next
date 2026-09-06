package admission

import "testing"

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
