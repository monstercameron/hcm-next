package schedule_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func occurrenceZone(t *testing.T) values.ZoneRef {
	t.Helper()
	return values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}
}

func occurrenceInstant(t *testing.T, year int, month time.Month, day, hour int) values.Instant {
	t.Helper()
	return values.NewInstant(time.Date(year, month, day, hour, 0, 0, 0, time.UTC))
}

func occurrenceWindow(t *testing.T, start, end time.Time) schedule.OccurrenceWindow {
	t.Helper()
	return schedule.OccurrenceWindow{Start: values.NewInstant(start), End: values.NewInstant(end)}
}

// TestTodo_SCHED_002 proves that a published CRON trigger is evaluated in an
// explicit tenant zone, including both sides of a fall-back fold, and that a
// resume applies the declared bounded misfire policy deterministically.
func TestTodo_SCHED_002(t *testing.T) {
	def, targets := fixture()
	def.ID = "dst-review"
	def.Source.Cron.Expression = "30 1 * * *"
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	request := schedule.OccurrenceRequest{
		Window:  occurrenceWindow(t, time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)),
		Zone:    occurrenceZone(t),
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpAll, Grace: time.Hour, MaxCatchUp: 4},
	}
	result, err := schedule.CalculateOccurrences(published, request)
	if err != nil {
		t.Fatalf("CalculateOccurrences: %v", err)
	}
	if len(result.Occurrences) != 2 {
		t.Fatalf("occurrences = %d, want both sides of DST fold", len(result.Occurrences))
	}
	if result.Occurrences[0].At.Compare(result.Occurrences[1].At) >= 0 {
		t.Fatalf("fold occurrences are not ordered: %s then %s", result.Occurrences[0].At, result.Occurrences[1].At)
	}
	if result.Occurrences[0].Key == result.Occurrences[1].Key {
		t.Fatal("DST fold occurrences must have distinct deterministic keys")
	}

	resume := occurrenceInstant(t, 2026, time.November, 2, 12)
	request.ResumeAt = resume
	request.Misfire = schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour, MaxCatchUp: 1}
	resumed, err := schedule.CalculateOccurrences(published, request)
	if err != nil {
		t.Fatalf("CalculateOccurrences with resume: %v", err)
	}
	if len(resumed.Occurrences) != 1 || resumed.Occurrences[0].Misfire != schedule.MisfireCatch {
		t.Fatalf("resumed occurrences = %+v, want one CATCH_UP occurrence", resumed.Occurrences)
	}
}

// TestTodo_SCHED_002_Golden pins the fold fixture's exact occurrence keys and
// result digest, including the values.ZonedDateTime disambiguation evidence.
func TestTodo_SCHED_002_Golden(t *testing.T) {
	def, targets := fixture()
	def.ID = "dst-golden"
	def.Source.Cron.Expression = "30 1 * * *"
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	result, err := schedule.CalculateOccurrences(published, schedule.OccurrenceRequest{
		Window:  occurrenceWindow(t, time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)),
		Zone:    occurrenceZone(t),
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
	})
	if err != nil {
		t.Fatalf("CalculateOccurrences: %v", err)
	}
	const wantDigest = "sha256:503948e3a378b63cfcbb8d8360cb8a733cc6680adecd86501f420eb1a07ccb58"
	if result.Digest != wantDigest {
		t.Fatalf("Digest = %q, want %q", result.Digest, wantDigest)
	}
	if result.Occurrences[0].ScheduledAt.Disambiguation() == result.Occurrences[1].ScheduledAt.Disambiguation() {
		t.Fatal("golden fold did not retain distinct disambiguation policies")
	}
}

func TestTodo_SCHED_002_Race(t *testing.T) {
	def, targets := fixture()
	def.ID = "race-occurrences"
	def.Source.Cron.Expression = "*/15 * * * *"
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	request := schedule.OccurrenceRequest{
		Window:  occurrenceWindow(t, time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)),
		Zone:    occurrenceZone(t),
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
	}
	const workers = 12
	digests := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := schedule.CalculateOccurrences(published, request)
			errs[i] = err
			if err == nil {
				digests[i] = result.Digest
			}
		}(i)
	}
	wg.Wait()
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("worker %d digest %q differs from %q", i, digests[i], digests[0])
		}
	}
}

func TestTodo_SCHED_002_Fault(t *testing.T) {
	def, targets := fixture()
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	base := schedule.OccurrenceRequest{
		Window:  occurrenceWindow(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)),
		Zone:    occurrenceZone(t),
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
	}
	if _, err := schedule.CalculateOccurrences(published, base); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	base.Zone = values.ZoneRef{}
	if _, err := schedule.CalculateOccurrences(published, base); !errors.Is(err, schedule.ErrOccurrenceZone) {
		t.Fatalf("missing zone error = %v, want ErrOccurrenceZone", err)
	}
	base.Zone = occurrenceZone(t)
	base.Misfire = schedule.MisfireConfig{Policy: schedule.MisfireCatchUp}
	if _, err := schedule.CalculateOccurrences(published, base); !errors.Is(err, schedule.ErrMisfireBound) {
		t.Fatalf("unbounded catch-up error = %v, want ErrMisfireBound", err)
	}
}

func TestTodo_SCHED_002_Mutation(t *testing.T) {
	def, targets := fixture()
	def.ID = "mutation-occurrences"
	def.Source.Cron.Expression = "0 9 * * *"
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	request := schedule.OccurrenceRequest{
		Window:  occurrenceWindow(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.January, 3, 0, 0, 0, 0, time.UTC)),
		Zone:    occurrenceZone(t),
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
	}
	first, err := schedule.CalculateOccurrences(published, request)
	if err != nil {
		t.Fatalf("first calculation: %v", err)
	}
	digest := first.Digest
	first.Occurrences[0].Key = "tampered"
	second, err := schedule.CalculateOccurrences(published, request)
	if err != nil {
		t.Fatalf("second calculation: %v", err)
	}
	if second.Digest != digest || second.Occurrences[0].Key == "tampered" {
		t.Fatalf("calculation was affected by mutation: first=%q second=%q", digest, second.Digest)
	}
}
