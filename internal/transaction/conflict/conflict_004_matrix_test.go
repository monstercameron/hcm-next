package conflict_test

import (
	"errors"
	"math/rand"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

func TestTodo_CONFLICT_004_Property(t *testing.T) {
	verdicts := map[conflict.PreflightVerdict]bool{
		conflict.PreflightClear: true, conflict.PreflightReapprovalRequired: true,
		conflict.PreflightReplanRequired: true, conflict.PreflightBlocked: true,
	}
	rng := rand.New(rand.NewSource(0xC004))
	for round := range 300 {
		pinned := []conflict.WriteIntent{conflict004Intent(t, "intent-a")}
		current := []conflict.WriteIntent{conflict004Intent(t, "intent-a")}
		if rng.Intn(3) == 0 {
			late := conflict004Intent(t, "intent-new")
			late.Footprints[0].Field = conflict.FieldPath("employment.assignment.manager_ref")
			current = append(current, late)
		}
		if rng.Intn(4) == 0 {
			current = current[:1]
		}
		preflight, err := conflict.ReevaluatePreflight(conflict.ReevaluateRequest{
			PolicyVersion: "v2", Pinned: pinned, Current: current,
		})
		if err != nil {
			t.Fatalf("round %d: unexpected error %v", round, err)
		}
		if !verdicts[preflight.Verdict] {
			t.Fatalf("round %d: unknown verdict %q", round, preflight.Verdict)
		}
		if preflight.Verdict == conflict.PreflightBlocked && len(preflight.Overlaps) == 0 {
			t.Fatalf("round %d: blocked without explanation", round)
		}
		if preflight.Verdict == conflict.PreflightClear &&
			(len(preflight.New) != 0 || len(preflight.Missing) != 0 || len(preflight.Changed) != 0) {
			t.Fatalf("round %d: clear with drift", round)
		}
	}
}

func TestTodo_CONFLICT_004_Race(t *testing.T) {
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			preflight, err := conflict.ReevaluatePreflight(conflict004Request(t))
			if err != nil {
				errs <- err
				return
			}
			digests <- preflight.Digest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent preflight failed: %v", err)
	}
	var first string
	for digest := range digests {
		if first == "" {
			first = digest
		} else if digest != first {
			t.Fatal("concurrent preflights diverged")
		}
	}
}

func TestTodo_CONFLICT_004_Mutation(t *testing.T) {
	t.Run("changed footprint needs replan", func(t *testing.T) {
		req := conflict004Request(t)
		req.Current[0].Footprints[0].Field = "employment.assignment.manager_ref"
		preflight, err := conflict.ReevaluatePreflight(req)
		if err != nil {
			t.Fatal(err)
		}
		if preflight.Verdict != conflict.PreflightReplanRequired {
			t.Fatalf("verdict = %s, want REPLAN_REQUIRED", preflight.Verdict)
		}
	})
	t.Run("changed interval needs replan", func(t *testing.T) {
		req := conflict004Request(t)
		altered := req.Current[0].Footprints[0]
		altered.ExpectedRevision = mustSequenceRevision(t, "people.employment.9001", 43)
		req.Current[0].Footprints[0] = altered
		preflight, err := conflict.ReevaluatePreflight(req)
		if err != nil {
			t.Fatal(err)
		}
		if preflight.Verdict != conflict.PreflightReplanRequired {
			t.Fatalf("verdict = %s, want REPLAN_REQUIRED", preflight.Verdict)
		}
	})
	t.Run("empty policy refused", func(t *testing.T) {
		req := conflict004Request(t)
		req.PolicyVersion = ""
		if _, err := conflict.ReevaluatePreflight(req); !errors.Is(err, conflict.ErrInvalidIntent) {
			t.Fatalf("empty policy accepted: %v", err)
		}
	})
	t.Run("blocked beats reapproval", func(t *testing.T) {
		req := conflict004Request(t)
		late := conflict004Intent(t, "intent-late")
		req.Current = append(req.Current, late)
		preflight, err := conflict.ReevaluatePreflight(req)
		if err != nil {
			t.Fatal(err)
		}
		if preflight.Verdict != conflict.PreflightBlocked {
			t.Fatalf("verdict = %s, want BLOCKED", preflight.Verdict)
		}
	})
}
