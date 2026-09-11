package schedule_test

import (
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

func TestTodo_SCHED_003_Race(t *testing.T) {
	dispatcher := dispatchDispatcher()
	base := occurrenceFiringAt(t, 1, 0)
	const workers = 16
	type result struct {
		outcome schedule.DispatchOutcome
		err     error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, err := dispatcher.Dispatch(base)
			results <- result{outcome, err}
		}()
	}
	wg.Wait()
	close(results)
	first := true
	var id string
	winners := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent Dispatch failed: %v", r.err)
		}
		if !r.outcome.Created || len(r.outcome.IntentIDs) != 1 {
			t.Fatalf("concurrent Dispatch diverged: %+v", r.outcome)
		}
		if first {
			id = r.outcome.IntentIDs[0]
			first = false
		}
		if r.outcome.IntentIDs[0] != id {
			t.Fatal("concurrent firings named different intents")
		}
		if !r.outcome.Duplicate {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d concurrent dispatches claimed first firing, want exactly 1", winners)
	}
	if dispatcher.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", dispatcher.Cursor())
	}
}

func TestTodo_SCHED_003_Integration(t *testing.T) {
	dispatcher := dispatchDispatcher()
	occurrence, err := dispatcher.Dispatch(occurrenceFiringAt(t, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	evented, err := dispatcher.Dispatch(eventFiring(2))
	if err != nil {
		t.Fatal(err)
	}
	stale := occurrenceFiringAt(t, 3, 1)
	stale.ObservedAt = dispatchClock().Add(-2 * time.Hour)
	receipt, err := dispatcher.Dispatch(stale)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Created || receipt.Receipt != "STALE" {
		t.Fatalf("stale firing not receipted: %+v", receipt)
	}
	replay, err := dispatcher.Dispatch(eventFiring(2))
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Duplicate || replay.IntentIDs[0] != evented.IntentIDs[0] {
		t.Fatal("event replay diverged across the pipeline")
	}
	if dispatcher.Cursor() != 3 {
		t.Fatalf("cursor = %d, want 3", dispatcher.Cursor())
	}
	if occurrence.IntentIDs[0] == evented.IntentIDs[0] {
		t.Fatal("occurrence and event share one intent identity")
	}
}

func TestTodo_SCHED_003_Fault(t *testing.T) {
	dispatcher := dispatchDispatcher()
	bad := occurrenceFiringAt(t, 1, 0)
	bad.Trigger.Definition.Owner = "mallory"
	if _, err := dispatcher.Dispatch(bad); err == nil {
		t.Fatal("tampered trigger dispatched")
	}
	if dispatcher.Cursor() != 0 {
		t.Fatalf("failed dispatch moved the cursor to %d", dispatcher.Cursor())
	}
	good, err := dispatcher.Dispatch(occurrenceFiringAt(t, 1, 0))
	if err != nil {
		t.Fatalf("valid firing after failure rejected: %v", err)
	}
	if !good.Created {
		t.Fatalf("valid firing not created: %+v", good)
	}
}

func TestTodo_SCHED_003_Mutation(t *testing.T) {
	t.Run("same-key old sequence replays", func(t *testing.T) {
		dispatcher := dispatchDispatcher()
		if _, err := dispatcher.Dispatch(occurrenceFiringAt(t, 4, 0)); err != nil {
			t.Fatal(err)
		}
		replay := occurrenceFiringAt(t, 2, 0)
		outcome, err := dispatcher.Dispatch(replay)
		if err != nil {
			t.Fatalf("same-key replay refused: %v", err)
		}
		if !outcome.Duplicate {
			t.Fatalf("same-key replay not marked duplicate: %+v", outcome)
		}
	})
	t.Run("review occurrence receipts", func(t *testing.T) {
		published := scheduledTrigger(t, "dispatch-review")
		occurrences := scheduledOccurrences(t, published)
		review := occurrences[0]
		review.Misfire = schedule.MisfireNeedsReview
		firing := occurrenceFiringAt(t, 1, 0)
		firing.Trigger = published
		firing.Occurrence = review
		firing.Key = review.Key
		outcome, err := dispatchDispatcher().Dispatch(firing)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Created || outcome.Receipt != string(schedule.ReceiptReviewRequired) {
			t.Fatalf("review firing not receipted: %+v", outcome)
		}
	})
	t.Run("unknown event evidenced", func(t *testing.T) {
		firing := eventFiring(1)
		firing.Observation.EventType = "payroll.hack"
		outcome, err := dispatchDispatcher().Dispatch(firing)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Created || outcome.Receipt != "UNKNOWN" {
			t.Fatalf("unknown event not evidenced: %+v", outcome)
		}
	})
}
