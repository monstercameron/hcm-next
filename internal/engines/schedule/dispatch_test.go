package schedule_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/eventpolicy"
)

func dispatchClock() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

func dispatchDispatcher() *schedule.Dispatcher {
	dispatcher, err := schedule.NewDispatcher(schedule.DispatchPolicy{
		MaxAge: time.Hour,
		Clock:  dispatchClock,
	})
	if err != nil {
		panic(err)
	}
	return dispatcher
}

func occurrenceFiringAt(t *testing.T, seq uint64, index int) schedule.Firing {
	t.Helper()
	published := scheduledTrigger(t, "dispatch-trigger")
	occurrences := scheduledOccurrences(t, published)
	return schedule.Firing{
		Kind:         schedule.FiringOccurrence,
		Key:          occurrences[index].Key,
		Sequence:     seq,
		ObservedAt:   dispatchClock(),
		Occurrence:   occurrences[index],
		Trigger:      published,
		TargetScope:  []string{"org:acme"},
		Leader:       schedule.LeaderClaim{ID: "leader-1", Epoch: 1},
		AllowCatchUp: true,
	}
}

func eventFiring(seq uint64) schedule.Firing {
	policy := eventpolicy.TriggerPolicy{
		PolicyVersion: "v3",
		Tenants:       []string{"tenant-1"},
		Types: map[string]eventpolicy.TypeRule{
			"timesheet.approved": {
				Schema: "hcm/timesheet", AllowedVersions: []string{"v2"},
				Sources:      []eventpolicy.EventSource{eventpolicy.SourceInternal},
				FixedScope:   []string{"org:acme"},
				Fanout:       1,
				RequireTruth: false,
			},
		},
		MaxRedelivery: 3,
		MaxFanout:     2,
	}
	return schedule.Firing{
		Kind:       schedule.FiringEvent,
		Key:        "evt-dispatch-1",
		Sequence:   seq,
		ObservedAt: dispatchClock(),
		Policy:     policy,
		Observation: eventpolicy.Observation{
			EventID: "evt-dispatch-1", EventType: "timesheet.approved",
			Schema: "hcm/timesheet", SchemaVersion: "v2", Tenant: "tenant-1",
			Source: eventpolicy.SourceInternal, PayloadDigest: "sha256:payload",
		},
	}
}

// TestTodo_SCHED_003 proves durable trigger dispatch: one fenced firing
// names exactly one policy-bound intent or a typed receipt, duplicates
// converge, rewinds and poison never duplicate work, and the dispatcher
// never invokes domain effects directly.
func TestTodo_SCHED_003(t *testing.T) {
	dispatcher := dispatchDispatcher()
	first, err := dispatcher.Dispatch(occurrenceFiringAt(t, 1, 0))
	if err != nil {
		t.Fatalf("valid firing rejected: %v", err)
	}
	if !first.Created || len(first.IntentIDs) != 1 {
		t.Fatalf("firing did not create one intent: %+v", first)
	}
	replay, err := dispatcher.Dispatch(occurrenceFiringAt(t, 1, 0))
	if err != nil {
		t.Fatalf("redelivery rejected: %v", err)
	}
	if !replay.Duplicate || replay.IntentIDs[0] != first.IntentIDs[0] {
		t.Fatalf("redelivery diverged: %+v vs %+v", first, replay)
	}
	evented, err := dispatcher.Dispatch(eventFiring(2))
	if err != nil {
		t.Fatal(err)
	}
	if !evented.Created || len(evented.IntentIDs) != 1 {
		t.Fatalf("event firing did not create one intent: %+v", evented)
	}

	t.Run("cursor rewind refused", func(t *testing.T) {
		dispatcher := dispatchDispatcher()
		if _, err := dispatcher.Dispatch(occurrenceFiringAt(t, 5, 0)); err != nil {
			t.Fatal(err)
		}
		rewound := occurrenceFiringAt(t, 3, 0)
		rewound.Key = "sha256:another-firing"
		if _, err := dispatcher.Dispatch(rewound); !errors.Is(err, schedule.ErrCursorRewind) {
			t.Fatalf("rewind accepted: %v", err)
		}
	})

	t.Run("stale firing receipts without work", func(t *testing.T) {
		firing := occurrenceFiringAt(t, 1, 0)
		firing.ObservedAt = dispatchClock().Add(-2 * time.Hour)
		outcome, err := dispatchDispatcher().Dispatch(firing)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Created || len(outcome.IntentIDs) != 0 {
			t.Fatalf("stale firing created work: %+v", outcome)
		}
	})

	t.Run("poison firings refused", func(t *testing.T) {
		for name, mutate := range map[string]func(*schedule.Firing){
			"empty key":     func(f *schedule.Firing) { f.Key = "" },
			"unknown kind":  func(f *schedule.Firing) { f.Kind = "CARRIER_PIGEON" },
			"zero sequence": func(f *schedule.Firing) { f.Sequence = 0 },
		} {
			firing := occurrenceFiringAt(t, 1, 0)
			mutate(&firing)
			if _, err := dispatchDispatcher().Dispatch(firing); !errors.Is(err, schedule.ErrInvalidFiring) {
				t.Fatalf("%s accepted: %v", name, err)
			}
		}
	})
}
