package schedule_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

func resumedOccurrences(t *testing.T, published schedule.PublishedTrigger, policy schedule.MisfirePolicy) []schedule.Occurrence {
	t.Helper()
	result, err := schedule.CalculateOccurrences(published, schedule.OccurrenceRequest{
		Window:   occurrenceWindow(t, time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)),
		Zone:     occurrenceZone(t),
		ResumeAt: occurrenceInstant(t, 2026, time.November, 2, 12),
		Misfire:  schedule.MisfireConfig{Policy: policy, Grace: time.Hour, MaxCatchUp: 4},
	})
	if err != nil {
		t.Fatalf("CalculateOccurrences with resume: %v", err)
	}
	return result.Occurrences
}

func TestTodo_INTENT_017_Golden(t *testing.T) {
	published := scheduledTrigger(t, "scheduled-golden")
	occurrences := scheduledOccurrences(t, published)
	conversion, err := schedule.NewConverter().Convert(conversionRequest(published, occurrences[0]))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(conversion, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/intent017_golden.json"
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

func TestTodo_INTENT_017_Race(t *testing.T) {
	published := scheduledTrigger(t, "scheduled-race")
	occurrences := scheduledOccurrences(t, published)
	converter := schedule.NewConverter()
	const workers = 16
	type result struct {
		conversion schedule.Conversion
		err        error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := conversionRequest(published, occurrences[0])
			req.Leader = schedule.LeaderClaim{ID: "leader-race", Epoch: uint64(i)}
			conversion, err := converter.Convert(req)
			results <- result{conversion, err}
		}(i)
	}
	wg.Wait()
	close(results)
	first := true
	var id string
	winners := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent Convert failed: %v", r.err)
		}
		if r.conversion.Intent == nil {
			t.Fatal("concurrent Convert produced no intent")
		}
		if first {
			id = r.conversion.Intent.IntentID
			first = false
		}
		if r.conversion.Intent.IntentID != id {
			t.Fatal("concurrent leaders named different intents")
		}
		if !r.conversion.Duplicate {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d concurrent conversions claimed first creation, want exactly 1", winners)
	}
}

func TestTodo_INTENT_017_Fault(t *testing.T) {
	published := scheduledTrigger(t, "scheduled-fault")
	occurrences := scheduledOccurrences(t, published)
	converter := schedule.NewConverter()
	cases := []struct {
		name   string
		mutate func(*schedule.ConversionRequest)
		cause  error
	}{
		{"empty occurrence key", func(r *schedule.ConversionRequest) { r.Occurrence.Key = "" }, schedule.ErrInvalidConversion},
		{"empty leader", func(r *schedule.ConversionRequest) { r.Leader.ID = "" }, schedule.ErrInvalidConversion},
		{"empty scope", func(r *schedule.ConversionRequest) { r.TargetScope = nil }, schedule.ErrInvalidConversion},
		{"wrong trigger", func(r *schedule.ConversionRequest) { r.Occurrence.Trigger.ID = "another-trigger" }, schedule.ErrTriggerMismatch},
		{"conflicting scope", func(r *schedule.ConversionRequest) { r.TargetScope = []string{"org:other"} }, schedule.ErrConversionConflict},
	}
	if _, err := converter.Convert(conversionRequest(published, occurrences[0])); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := conversionRequest(published, occurrences[0])
			tc.mutate(&req)
			if _, err := converter.Convert(req); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}
	t.Run("tampered trigger refused", func(t *testing.T) {
		req := conversionRequest(published, occurrences[0])
		req.Trigger.Definition.Owner = "mallory"
		if _, err := schedule.NewConverter().Convert(req); !errors.Is(err, schedule.ErrInvalidConversion) {
			t.Fatalf("tampered trigger accepted")
		}
	})
}

func TestTodo_INTENT_017_Recovery(t *testing.T) {
	published := scheduledTrigger(t, "scheduled-recovery")
	occurrences := scheduledOccurrences(t, published)
	converter := schedule.NewConverter()
	bad := conversionRequest(published, occurrences[0])
	bad.Occurrence.Trigger.ID = "another-trigger"
	if _, err := converter.Convert(bad); !errors.Is(err, schedule.ErrTriggerMismatch) {
		t.Fatalf("want mismatch refusal, got %v", err)
	}
	good, err := converter.Convert(conversionRequest(published, occurrences[0]))
	if err != nil {
		t.Fatalf("valid conversion after refusal failed: %v", err)
	}
	if good.Intent == nil || good.Duplicate {
		t.Fatalf("refusal left state behind: %+v", good)
	}
	skipped, err := converter.Convert(conversionRequest(published, occurrences[0]))
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Intent == nil || skipped.Intent.IntentID != good.Intent.IntentID {
		t.Fatal("receipt path diverged from the created intent")
	}
}

func TestTodo_INTENT_017_Mutation(t *testing.T) {
	published := scheduledTrigger(t, "scheduled-mutation")
	frozen := scheduledOccurrences(t, published)
	skipped := frozen[0]
	skipped.Misfire = schedule.MisfireSkipped
	conversion, err := schedule.NewConverter().Convert(conversionRequest(published, skipped))
	if err != nil {
		t.Fatal(err)
	}
	if conversion.Intent != nil || conversion.Receipt == nil || conversion.Receipt.Disposition != schedule.ReceiptSkipped {
		t.Fatalf("SKIP occurrence did not yield a SKIPPED receipt: %+v", conversion)
	}
	caught := resumedOccurrences(t, published, schedule.MisfireCatchUpOnce)
	if len(caught) == 0 || caught[0].Misfire != schedule.MisfireCatch {
		t.Fatalf("resume did not produce CATCH_UP occurrences: %+v", caught)
	}
	fired, err := schedule.NewConverter().Convert(conversionRequest(published, caught[0]))
	if err != nil {
		t.Fatal(err)
	}
	if fired.Intent == nil {
		t.Fatalf("allowed catch-up created no intent: %+v", fired)
	}
	gated := conversionRequest(published, caught[0])
	gated.AllowCatchUp = false
	deferred, err := schedule.NewConverter().Convert(gated)
	if err != nil {
		t.Fatal(err)
	}
	if deferred.Intent != nil || deferred.Receipt == nil || deferred.Receipt.Disposition != schedule.ReceiptDeferred {
		t.Fatalf("gated catch-up did not yield a DEFERRED receipt: %+v", deferred)
	}
	t.Run("review occurrence needs review", func(t *testing.T) {
		occurrences := scheduledOccurrences(t, published)
		review := occurrences[0]
		review.Misfire = schedule.MisfireNeedsReview
		conversion, err := schedule.NewConverter().Convert(conversionRequest(published, review))
		if err != nil {
			t.Fatal(err)
		}
		if conversion.Intent != nil || conversion.Receipt == nil || conversion.Receipt.Disposition != schedule.ReceiptReviewRequired {
			t.Fatalf("REVIEW occurrence did not yield a REVIEW_REQUIRED receipt: %+v", conversion)
		}
	})
	t.Run("revised schedule needs review", func(t *testing.T) {
		def, targets := fixture()
		def.ID = "scheduled-revision"
		def.Version = "1"
		def.Source.Cron.Expression = "30 1 * * *"
		first, err := schedule.Publish(schedule.NewRegistry(), def, targets)
		if err != nil {
			t.Fatal(err)
		}
		occurrences := scheduledOccurrences(t, first)
		def.Version = "2"
		second, err := schedule.Publish(schedule.NewRegistry(), def, targets)
		if err != nil {
			t.Fatal(err)
		}
		conversion, err := schedule.NewConverter().Convert(conversionRequest(second, occurrences[0]))
		if err != nil {
			t.Fatal(err)
		}
		if conversion.Intent != nil || conversion.Receipt == nil || conversion.Receipt.Disposition != schedule.ReceiptReviewRequired {
			t.Fatalf("revised schedule did not yield a REVIEW_REQUIRED receipt: %+v", conversion)
		}
	})
}
