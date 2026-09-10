package parallel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func admittedBranches() []Branch {
	return []Branch{
		{ID: "b1", IdempotencyKey: "k1", Writes: []string{"w1"}, Cost: 2, Admitted: true, Work: func(ctx context.Context) (string, error) { return "one", nil }},
		{ID: "b2", IdempotencyKey: "k2", Writes: []string{"w2"}, Cost: 3, Admitted: true, Work: func(ctx context.Context) (string, error) { return "two", nil }},
		{ID: "b3", IdempotencyKey: "k3", Writes: []string{"w3"}, Cost: 1, Admitted: true, Work: func(ctx context.Context) (string, error) { return "three", nil }},
	}
}

func boundedSpec() Spec {
	return Spec{Branches: admittedBranches(), MaxBranches: 3, Budget: 10, FailurePolicy: CollectAll}
}

func TestTodo_WF_STEP_007(t *testing.T) {
	report, err := Execute(context.Background(), boundedSpec())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(report.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(report.Results))
	}
	for i, result := range report.Results {
		if result.Outcome != OutcomeSucceeded || result.IdempotencyKey == "" {
			t.Fatalf("result %d = %+v", i, result)
		}
	}
	// RED: every bound violation refuses before any branch runs.
	var ran atomic.Int64
	counting := boundedSpec()
	for i := range counting.Branches {
		branch := counting.Branches[i]
		branch.Work = func(ctx context.Context) (string, error) { ran.Add(1); return "", nil }
		counting.Branches[i] = branch
	}
	cases := map[string]func(*Spec){
		"unbounded count": func(s *Spec) { s.Branches = append(s.Branches, s.Branches[0]) },
		"zero bound":      func(s *Spec) { s.MaxBranches = 0 },
		"empty branches":  func(s *Spec) { s.Branches = nil },
		"conflicting writes": func(s *Spec) {
			s.Branches = append(s.Branches[:2:2], Branch{ID: "bx", IdempotencyKey: "kx", Writes: []string{"w1"}, Admitted: true, Work: counting.Branches[0].Work})
		},
		"exceeded budget":       func(s *Spec) { s.Budget = 5 },
		"undefined propagation": func(s *Spec) { s.FailurePolicy = "MAYBE" },
		"unadmitted branch":     func(s *Spec) { s.Branches[1].Admitted = false },
		"duplicate identity":    func(s *Spec) { s.Branches[2].ID = "b1" },
		"duplicate idempotency": func(s *Spec) { s.Branches[2].IdempotencyKey = "k1" },
	}
	for name, mutate := range cases {
		spec := counting
		spec.Branches = append([]Branch(nil), counting.Branches...)
		mutate(&spec)
		if _, err := Execute(context.Background(), spec); err == nil {
			t.Fatalf("%s executed", name)
		}
	}
	if ran.Load() != 0 {
		t.Fatalf("refused specs ran %d branches", ran.Load())
	}
	// Failure and cancel propagation under FAIL_FAST.
	branches := []Branch{
		{ID: "fail", IdempotencyKey: "kf", Admitted: true, Work: func(ctx context.Context) (string, error) { return "", errors.New("boom") }},
		{ID: "wait", IdempotencyKey: "kw", Admitted: true, Work: func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() }},
	}
	fast, err := Execute(context.Background(), Spec{Branches: branches, MaxBranches: 2, Budget: 10, FailurePolicy: FailFast})
	if err != nil {
		t.Fatalf("fail-fast Execute: %v", err)
	}
	byID := map[string]BranchResult{}
	for _, result := range fast.Results {
		byID[result.BranchID] = result
	}
	if byID["fail"].Outcome != OutcomeFailed || byID["wait"].Outcome != OutcomeCancelled {
		t.Fatalf("fail-fast = %+v", fast.Results)
	}
	// COLLECT_ALL lets every branch reach its own outcome.
	independent := []Branch{
		{ID: "bad", IdempotencyKey: "kb", Admitted: true, Work: func(ctx context.Context) (string, error) { return "", errors.New("boom") }},
		{ID: "good", IdempotencyKey: "kg", Admitted: true, Work: func(ctx context.Context) (string, error) { return "fine", nil }},
	}
	collected, err := Execute(context.Background(), Spec{Branches: independent, MaxBranches: 2, Budget: 10, FailurePolicy: CollectAll})
	if err != nil {
		t.Fatalf("collect Execute: %v", err)
	}
	byCollect := map[string]BranchResult{}
	for _, result := range collected.Results {
		byCollect[result.BranchID] = result
	}
	if byCollect["bad"].Outcome != OutcomeFailed || byCollect["good"].Outcome != OutcomeSucceeded {
		t.Fatalf("collect-all = %+v", collected.Results)
	}
}

func TestTodo_WF_STEP_007_Race(t *testing.T) {
	var total atomic.Int64
	const workers = 16
	var wg sync.WaitGroup
	reports := make([]Report, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			spec := boundedSpec()
			for j := range spec.Branches {
				branch := spec.Branches[j]
				branch.ID = fmt.Sprintf("w%d-%s", i, branch.ID)
				branch.IdempotencyKey = fmt.Sprintf("w%d-%s", i, branch.IdempotencyKey)
				branch.Work = func(ctx context.Context) (string, error) { total.Add(1); return "ok", nil }
				spec.Branches[j] = branch
			}
			reports[i], errs[i] = Execute(context.Background(), spec)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if len(reports[i].Results) != 3 {
			t.Fatalf("worker %d results = %d", i, len(reports[i].Results))
		}
		for _, result := range reports[i].Results {
			if result.Outcome != OutcomeSucceeded {
				t.Fatalf("worker %d result = %+v", i, result)
			}
		}
	}
	if total.Load() != workers*3 {
		t.Fatalf("executed %d branches, want %d", total.Load(), workers*3)
	}
}

func TestTodo_WF_STEP_007_Mutation(t *testing.T) {
	// One flipped write key creates a conflict that refuses.
	conflict := boundedSpec()
	conflict.Branches[2].Writes = []string{"w1"}
	if _, err := Execute(context.Background(), conflict); !errors.Is(err, ErrWriteConflict) {
		t.Fatalf("write conflict err = %v", err)
	}
	// One lowered budget refuses the same admitted work.
	starved := boundedSpec()
	starved.Budget = 5
	if _, err := Execute(context.Background(), starved); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("budget err = %v", err)
	}
	// One flipped outcome propagates under fail-fast.
	branches := []Branch{
		{ID: "ok", IdempotencyKey: "ko", Admitted: true, Work: func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() }},
		{ID: "flipped", IdempotencyKey: "kf", Admitted: true, Work: func(ctx context.Context) (string, error) { return "", errors.New("mutated failure") }},
	}
	report, err := Execute(context.Background(), Spec{Branches: branches, MaxBranches: 2, Budget: 4, FailurePolicy: FailFast})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	byID := map[string]BranchResult{}
	for _, result := range report.Results {
		byID[result.BranchID] = result
	}
	if byID["flipped"].Outcome != OutcomeFailed || byID["ok"].Outcome != OutcomeCancelled {
		t.Fatalf("mutated propagation = %+v", report.Results)
	}
}
