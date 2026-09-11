package schedule_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

func scheduledTrigger(t *testing.T, id string) schedule.PublishedTrigger {
	t.Helper()
	def, targets := fixture()
	def.ID = id
	def.Source.Cron.Expression = "30 1 * * *"
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return published
}

func scheduledOccurrences(t *testing.T, published schedule.PublishedTrigger) []schedule.Occurrence {
	t.Helper()
	result, err := schedule.CalculateOccurrences(published, schedule.OccurrenceRequest{
		Window:  occurrenceWindow(t, time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)),
		Zone:    occurrenceZone(t),
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
	})
	if err != nil {
		t.Fatalf("CalculateOccurrences: %v", err)
	}
	if len(result.Occurrences) != 2 {
		t.Fatalf("occurrences = %d, want both sides of the DST fold", len(result.Occurrences))
	}
	return result.Occurrences
}

func conversionRequest(published schedule.PublishedTrigger, occurrence schedule.Occurrence) schedule.ConversionRequest {
	return schedule.ConversionRequest{
		Trigger:      published,
		Occurrence:   occurrence,
		TargetScope:  []string{"org:acme"},
		Leader:       schedule.LeaderClaim{ID: "leader-1", Epoch: 7},
		AllowCatchUp: true,
	}
}

// TestScheduledIntentCreationHandlesMisfireDSTAndReplay proves one schedule
// occurrence becomes exactly one intent: DST-fold twins name distinct
// intents, replays and leader overlaps return the identical intent, and
// every misfire disposition carries a typed receipt instead of inventing
// domain work.
func TestScheduledIntentCreationHandlesMisfireDSTAndReplay(t *testing.T) {
	published := scheduledTrigger(t, "scheduled-intents")
	occurrences := scheduledOccurrences(t, published)

	converter := schedule.NewConverter()
	first, err := converter.Convert(conversionRequest(published, occurrences[0]))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if first.Intent == nil || first.Receipt != nil || first.Duplicate {
		t.Fatalf("first conversion is not a fresh intent: %+v", first)
	}
	replay, err := converter.Convert(conversionRequest(published, occurrences[0]))
	if err != nil {
		t.Fatalf("replay Convert: %v", err)
	}
	if replay.Intent == nil || !replay.Duplicate || replay.Intent.IntentID != first.Intent.IntentID {
		t.Fatalf("replay did not return the identical intent: %+v vs %+v", first, replay)
	}
	twin, err := converter.Convert(conversionRequest(published, occurrences[1]))
	if err != nil {
		t.Fatalf("twin Convert: %v", err)
	}
	if twin.Intent == nil || twin.Intent.IntentID == first.Intent.IntentID {
		t.Fatal("DST-fold twins share one intent")
	}
	overlap := conversionRequest(published, occurrences[0])
	overlap.Leader = schedule.LeaderClaim{ID: "leader-2", Epoch: 3}
	overlapped, err := converter.Convert(overlap)
	if err != nil {
		t.Fatalf("overlap Convert: %v", err)
	}
	if overlapped.Intent == nil || overlapped.Intent.IntentID != first.Intent.IntentID {
		t.Fatalf("leader overlap duplicated the intent: %+v", overlapped)
	}
	fresh := schedule.NewConverter()
	crashed, err := fresh.Convert(conversionRequest(published, occurrences[0]))
	if err != nil {
		t.Fatalf("post-crash Convert: %v", err)
	}
	if crashed.Intent == nil || crashed.Intent.IntentID != first.Intent.IntentID {
		t.Fatal("post-crash replay derived a different intent")
	}
}
