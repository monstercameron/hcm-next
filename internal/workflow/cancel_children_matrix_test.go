package workflow_test

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WF_RUN_011_Golden(t *testing.T) {
	outcome, err := workflow.PropagateCancellationToChildren(propagationRequest())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(outcome, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/cancel_children_golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_RUN_011_Race(t *testing.T) {
	const workers = 16
	type result struct {
		outcome workflow.ChildPropagationOutcome
		err     error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, err := workflow.PropagateCancellationToChildren(propagationRequest())
			results <- result{outcome, err}
		}()
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent propagation failed: %v", r.err)
		}
		if r.outcome.Decision != workflow.Cancelled || len(r.outcome.Completion.Children) != 2 {
			t.Fatalf("concurrent propagation diverged: %+v", r.outcome)
		}
	}
}

func TestTodo_WF_RUN_011_Fault(t *testing.T) {
	if _, err := workflow.PropagateCancellationToChildren(workflow.ParentCancellationRequest{}); err == nil {
		t.Fatal("identity-free propagation accepted")
	}
	empty, err := workflow.PropagateCancellationToChildren(workflow.ParentCancellationRequest{
		RunID: "run/empty", Revision: "rev1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Decision != workflow.Cancelled {
		t.Fatalf("childless parent = %s, want CANCELLED", empty.Decision)
	}
}

func TestTodo_WF_RUN_011_Mutation(t *testing.T) {
	t.Run("compensated child flags compensation", func(t *testing.T) {
		req := propagationRequest()
		req.Children[1].State = "COMPENSATED"
		outcome, err := workflow.PropagateCancellationToChildren(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.CompensationRequired {
			t.Fatalf("decision = %s, want COMPENSATION_REQUIRED", outcome.Decision)
		}
	})
	t.Run("uncancellable child refuses", func(t *testing.T) {
		req := propagationRequest()
		req.Children[0].Cancellable = false
		outcome, err := workflow.PropagateCancellationToChildren(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.CannotCancel {
			t.Fatalf("decision = %s, want CANNOT_CANCEL", outcome.Decision)
		}
	})
	t.Run("truth never overwritten", func(t *testing.T) {
		outcome, err := workflow.PropagateCancellationToChildren(propagationRequest())
		if err != nil {
			t.Fatal(err)
		}
		for _, truth := range outcome.Completion.Children {
			if truth.Outcome == "" {
				t.Fatalf("empty truth recorded: %+v", truth)
			}
		}
	})
}
