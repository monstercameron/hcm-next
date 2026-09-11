package workflow_test

import (
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WF_RUN_010_Race(t *testing.T) {
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, err := workflow.DecideCancellation(cancellationRequest())
			if err != nil {
				errs <- err
				return
			}
			digests <- outcome.Digest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent decision failed: %v", err)
	}
	var first string
	for digest := range digests {
		if first == "" {
			first = digest
		} else if digest != first {
			t.Fatal("concurrent decisions diverged")
		}
	}
}

func TestTodo_WF_RUN_010_Fault(t *testing.T) {
	for _, mutate := range []struct {
		name   string
		mutate func(*workflow.CancellationRequest)
	}{
		{"empty revision", func(r *workflow.CancellationRequest) { r.Revision = "" }},
		{"empty phase", func(r *workflow.CancellationRequest) { r.Phase = "" }},
	} {
		req := cancellationRequest()
		mutate.mutate(&req)
		if _, err := workflow.DecideCancellation(req); err == nil {
			t.Fatalf("%s accepted", mutate.name)
		}
	}
	empty, err := workflow.DecideCancellation(workflow.CancellationRequest{
		RunID: "run/empty", Revision: "rev1", Phase: "IDLE",
	})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Decision != workflow.Cancelled {
		t.Fatalf("effect-free idle run = %s, want CANCELLED", empty.Decision)
	}
}

func TestTodo_WF_RUN_010_Mutation(t *testing.T) {
	t.Run("compensated child cancels clean", func(t *testing.T) {
		req := cancellationRequest()
		req.Children[1].State = "COMPENSATED"
		outcome, err := workflow.DecideCancellation(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.Cancelled {
			t.Fatalf("decision = %s, want CANCELLED", outcome.Decision)
		}
	})
	t.Run("already-complete children cancel clean", func(t *testing.T) {
		req := cancellationRequest()
		req.Children[0].State = "SUCCEEDED"
		outcome, err := workflow.DecideCancellation(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.Cancelled {
			t.Fatalf("decision = %s, want CANCELLED", outcome.Decision)
		}
	})
	t.Run("decision binds run identity", func(t *testing.T) {
		first, err := workflow.DecideCancellation(cancellationRequest())
		if err != nil {
			t.Fatal(err)
		}
		req := cancellationRequest()
		req.RunID = "run/other"
		second, err := workflow.DecideCancellation(req)
		if err != nil {
			t.Fatal(err)
		}
		if first.Digest == second.Digest {
			t.Fatal("different runs share one decision digest")
		}
	})
}
